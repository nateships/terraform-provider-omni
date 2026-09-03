// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"context"
	"math"

	"github.com/cosi-project/runtime/pkg/safe"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/resourcevalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

// Ensure the resource satisfies the framework interfaces.
var (
	_ resource.Resource                     = (*machineClassResource)(nil)
	_ resource.ResourceWithConfigure        = (*machineClassResource)(nil)
	_ resource.ResourceWithImportState      = (*machineClassResource)(nil)
	_ resource.ResourceWithConfigValidators = (*machineClassResource)(nil)
)

// machineClassResourceModel maps the omni_machine_class resource schema.
type machineClassResourceModel struct {
	AutoProvision *machineClassAutoProvisionModel `tfsdk:"auto_provision"`
	MatchLabels   types.List                      `tfsdk:"match_labels"`
	Name          types.String                    `tfsdk:"name"`
}

// machineClassAutoProvisionModel maps the nested `auto_provision` block: it configures an
// infrastructure provider to provision machines for the class on demand.
type machineClassAutoProvisionModel struct {
	ProviderID   types.String                 `tfsdk:"provider_id"`
	ProviderData types.String                 `tfsdk:"provider_data"`
	KernelArgs   types.List                   `tfsdk:"kernel_args"`
	MetaValues   []machineClassMetaValueModel `tfsdk:"meta_values"`
	GRPCTunnel   types.Bool                   `tfsdk:"grpc_tunnel"`
}

// machineClassMetaValueModel maps one `meta_values` element: a Talos META partition entry written
// to the provisioned machines.
type machineClassMetaValueModel struct {
	Value types.String `tfsdk:"value"`
	Key   types.Int64  `tfsdk:"key"`
}

// machineClassResource implements the omni_machine_class resource.
type machineClassResource struct {
	data *providerData
}

// NewMachineClassResource returns a new omni_machine_class resource.
func NewMachineClassResource() resource.Resource {
	return &machineClassResource{}
}

// Metadata implements resource.Resource.
func (r *machineClassResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_machine_class"
}

// Schema implements resource.Resource.
func (r *machineClassResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages an Omni machine class: a reusable machine selector that machine sets scale against. " +
			"A class either matches existing machines by labels (`match_labels`) or asks an infrastructure provider " +
			"to provision machines on demand (`auto_provision`).",
		Attributes: map[string]schema.Attribute{
			"name": schema.StringAttribute{
				Required:    true,
				Description: "The machine class name, referenced by machine sets. Immutable.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"match_labels": schema.ListAttribute{
				Optional:    true,
				ElementType: types.StringType,
				Description: "Label selector expressions matching existing machines into the class " +
					"(e.g. `amd64`, `site = home`).",
				Validators: []validator.List{
					listvalidator.SizeAtLeast(1),
				},
			},
			"auto_provision": schema.SingleNestedAttribute{
				Optional:    true,
				Description: "Provision machines for the class on demand through an infrastructure provider.",
				Attributes: map[string]schema.Attribute{
					"provider_id": schema.StringAttribute{
						Required:    true,
						Description: "The infrastructure provider that provisions the machines (e.g. `proxmox`, `bare-metal`).",
					},
					"provider_data": schema.StringAttribute{
						Optional:    true,
						Description: "Provider-specific machine configuration, passed verbatim (usually YAML).",
						Validators: []validator.String{
							stringvalidator.LengthAtLeast(1),
						},
					},
					"kernel_args": schema.ListAttribute{
						Optional:    true,
						ElementType: types.StringType,
						Description: "Extra kernel arguments for the provisioned machines.",
						Validators: []validator.List{
							listvalidator.SizeAtLeast(1),
						},
					},
					"meta_values": schema.ListNestedAttribute{
						Optional:    true,
						Description: "Talos META partition entries written to the provisioned machines.",
						Validators: []validator.List{
							listvalidator.SizeAtLeast(1),
						},
						NestedObject: schema.NestedAttributeObject{
							Attributes: map[string]schema.Attribute{
								"key": schema.Int64Attribute{
									Required:    true,
									Description: "The META key (e.g. `0x0a` for the hostname).",
									Validators: []validator.Int64{
										int64validator.Between(0, math.MaxUint8),
									},
								},
								"value": schema.StringAttribute{
									Required:    true,
									Description: "The META value.",
								},
							},
						},
					},
					"grpc_tunnel": schema.BoolAttribute{
						Optional: true,
						Description: "Configure Talos to tunnel SideroLink management traffic over HTTP/2 instead of WireGuard's UDP. " +
							"Leave unset to inherit the Omni instance default. Only enable this when the network blocks UDP: the " +
							"tunnel adds significant overhead.",
					},
				},
			},
		},
	}
}

// ConfigValidators implements resource.ResourceWithConfigValidators. A class either matches
// existing machines or auto-provisions new ones; Omni ignores match labels on an auto-provisioning
// class, so configuring both hides a mistake.
func (r *machineClassResource) ConfigValidators(_ context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{
		resourcevalidator.Conflicting(
			path.MatchRoot("match_labels"),
			path.MatchRoot("auto_provision"),
		),
		resourcevalidator.AtLeastOneOf(
			path.MatchRoot("match_labels"),
			path.MatchRoot("auto_provision"),
		),
	}
}

// Configure implements resource.ResourceWithConfigure.
func (r *machineClassResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFromResource(req.ProviderData, &resp.Diagnostics)
}

// applySpec copies the plan onto the machine class spec.
func (r *machineClassResource) applySpec(ctx context.Context, plan machineClassResourceModel, mc *omni.MachineClass, diags *diag.Diagnostics) {
	spec := mc.TypedSpec().Value

	var matchLabels []string

	if !plan.MatchLabels.IsNull() {
		diags.Append(plan.MatchLabels.ElementsAs(ctx, &matchLabels, false)...)
	}

	spec.MatchLabels = matchLabels
	spec.AutoProvision = nil

	if plan.AutoProvision != nil {
		provision := specs.MachineClassSpec_Provision{
			ProviderId:   plan.AutoProvision.ProviderID.ValueString(),
			ProviderData: plan.AutoProvision.ProviderData.ValueString(),
		}

		if !plan.AutoProvision.KernelArgs.IsNull() {
			diags.Append(plan.AutoProvision.KernelArgs.ElementsAs(ctx, &provision.KernelArgs, false)...)
		}

		for _, mv := range plan.AutoProvision.MetaValues {
			provision.MetaValues = append(provision.MetaValues, &specs.MetaValue{
				Key:   uint32(mv.Key.ValueInt64()),
				Value: mv.Value.ValueString(),
			})
		}

		provision.GrpcTunnel = grpcTunnelMode(plan.AutoProvision.GRPCTunnel)

		spec.AutoProvision = &provision
	}
}

// Create implements resource.Resource.
func (r *machineClassResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan machineClassResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	mc := omni.NewMachineClass(plan.Name.ValueString())

	r.applySpec(ctx, plan, mc, &resp.Diagnostics)

	if resp.Diagnostics.HasError() {
		return
	}

	if err := r.data.state.Create(ctx, mc); err != nil {
		errToDiag(&resp.Diagnostics, "Failed to create Omni machine class", err)

		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements resource.Resource.
func (r *machineClassResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state machineClassResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	mc, err := safe.ReaderGetByID[*omni.MachineClass](ctx, r.data.state, state.Name.ValueString())
	if err != nil {
		if cosistate.IsNotFoundError(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		errToDiag(&resp.Diagnostics, "Failed to read Omni machine class", err)

		return
	}

	r.specToModel(ctx, mc, &state, &resp.Diagnostics)

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements resource.Resource. The name forces replacement; everything else updates in
// place.
func (r *machineClassResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan machineClassResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	updateWithDiags(ctx, r.data.state, omni.NewMachineClass(plan.Name.ValueString()).Metadata(), &resp.Diagnostics,
		"Failed to update Omni machine class",
		func(mc *omni.MachineClass) {
			r.applySpec(ctx, plan, mc, &resp.Diagnostics)
		})

	if resp.Diagnostics.HasError() {
		return
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete implements resource.Resource.
func (r *machineClassResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state machineClassResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	mc := omni.NewMachineClass(state.Name.ValueString())

	if err := r.data.state.TeardownAndDestroy(ctx, mc.Metadata()); err != nil {
		if cosistate.IsNotFoundError(err) {
			return
		}

		errToDiag(&resp.Diagnostics, "Failed to destroy Omni machine class", err)

		return
	}
}

// ImportState implements resource.ResourceWithImportState. Machine classes are imported by name.
func (r *machineClassResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("name"), req, resp)
}

// specToModel populates the model from a MachineClass resource read from Omni.
func (r *machineClassResource) specToModel(ctx context.Context, mc *omni.MachineClass, model *machineClassResourceModel, diags *diag.Diagnostics) {
	model.Name = types.StringValue(mc.Metadata().ID())

	spec := mc.TypedSpec().Value

	// An absent list stays null so a config that omits it does not produce spurious diffs.
	model.MatchLabels = types.ListNull(types.StringType)

	if len(spec.GetMatchLabels()) > 0 {
		list, d := types.ListValueFrom(ctx, types.StringType, spec.GetMatchLabels())
		diags.Append(d...)

		model.MatchLabels = list
	}

	model.AutoProvision = nil

	if provision := spec.GetAutoProvision(); provision != nil {
		auto := machineClassAutoProvisionModel{
			ProviderID: types.StringValue(provision.GetProviderId()),
			KernelArgs: types.ListNull(types.StringType),
		}

		auto.ProviderData = types.StringNull()
		if provision.GetProviderData() != "" {
			auto.ProviderData = types.StringValue(provision.GetProviderData())
		}

		if len(provision.GetKernelArgs()) > 0 {
			list, d := types.ListValueFrom(ctx, types.StringType, provision.GetKernelArgs())
			diags.Append(d...)

			auto.KernelArgs = list
		}

		for _, mv := range provision.GetMetaValues() {
			auto.MetaValues = append(auto.MetaValues, machineClassMetaValueModel{
				Key:   types.Int64Value(int64(mv.GetKey())),
				Value: types.StringValue(mv.GetValue()),
			})
		}

		switch provision.GetGrpcTunnel() {
		case specs.GrpcTunnelMode_ENABLED:
			auto.GRPCTunnel = types.BoolValue(true)
		case specs.GrpcTunnelMode_DISABLED:
			auto.GRPCTunnel = types.BoolValue(false)
		case specs.GrpcTunnelMode_UNSET:
			fallthrough
		default:
			auto.GRPCTunnel = types.BoolNull()
		}

		model.AutoProvision = &auto
	}
}
