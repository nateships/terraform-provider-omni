// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"context"
	"errors"
	"strings"

	"github.com/cosi-project/runtime/pkg/safe"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

// Ensure the resource satisfies the framework interfaces.
var (
	_ resource.Resource                = (*etcdBackupS3ConfigResource)(nil)
	_ resource.ResourceWithConfigure   = (*etcdBackupS3ConfigResource)(nil)
	_ resource.ResourceWithImportState = (*etcdBackupS3ConfigResource)(nil)
)

// etcdBackupS3ConfigResourceModel maps the omni_etcd_backup_s3_config resource schema.
type etcdBackupS3ConfigResourceModel struct {
	ID              types.String `tfsdk:"id"`
	Bucket          types.String `tfsdk:"bucket"`
	Region          types.String `tfsdk:"region"`
	Endpoint        types.String `tfsdk:"endpoint"`
	AccessKeyID     types.String `tfsdk:"access_key_id"`
	SecretAccessKey types.String `tfsdk:"secret_access_key"`
	SessionToken    types.String `tfsdk:"session_token"`
}

// etcdBackupS3ConfigResource implements the omni_etcd_backup_s3_config resource.
type etcdBackupS3ConfigResource struct {
	data *providerData
}

// NewEtcdBackupS3ConfigResource returns a new omni_etcd_backup_s3_config resource.
func NewEtcdBackupS3ConfigResource() resource.Resource {
	return &etcdBackupS3ConfigResource{}
}

// Metadata implements resource.Resource.
func (r *etcdBackupS3ConfigResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_etcd_backup_s3_config"
}

// Schema implements resource.Resource.
func (r *etcdBackupS3ConfigResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Manages the S3 storage Omni uses for etcd backups. This is an instance-wide singleton: at most one " +
			"`omni_etcd_backup_s3_config` may exist per Omni instance. Per-cluster backup scheduling is configured with the " +
			"`backup_interval` attribute of `omni_cluster`.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "The resource ID. Always `" + omni.EtcdBackupS3ConfID + "`, as the configuration is a singleton.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"bucket": schema.StringAttribute{
				Required:    true,
				Description: "The S3 bucket backups are written to.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"region": schema.StringAttribute{
				Optional: true,
				Description: "The region of the bucket, e.g. `us-east-1`. Leave unset for S3-compatible storage that does " +
					"not use regions.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"endpoint": schema.StringAttribute{
				Optional: true,
				Description: "Custom S3 endpoint, e.g. `https://s3.example.com`. Leave unset to use the AWS endpoint " +
					"for the configured region. Note that Omni disables TLS certificate verification for endpoints " +
					"with an `http://` scheme.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
				},
			},
			"access_key_id": schema.StringAttribute{
				Optional:  true,
				Sensitive: true,
				Description: "Static access key ID. Must be set together with `secret_access_key`. Leaving both unset is " +
					"only useful on Omni deployments that supply S3 credentials to the backend themselves.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("secret_access_key")),
				},
			},
			"secret_access_key": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Static secret access key. Required when `access_key_id` is set.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("access_key_id")),
				},
			},
			"session_token": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "Session token for temporary credentials. Only meaningful together with `access_key_id` and `secret_access_key`.",
				Validators: []validator.String{
					stringvalidator.LengthAtLeast(1),
					stringvalidator.AlsoRequires(path.MatchRoot("access_key_id")),
				},
			},
		},
	}
}

// Configure implements resource.ResourceWithConfigure.
func (r *etcdBackupS3ConfigResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = providerDataFromResource(req.ProviderData, &resp.Diagnostics)
}

// Create implements resource.Resource.
func (r *etcdBackupS3ConfigResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan etcdBackupS3ConfigResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	etcdBackupS3Config := omni.NewEtcdBackupS3Conf()

	if err := r.applyEtcdBackupConfigModel(plan, etcdBackupS3Config); err != nil {
		errToDiag(&resp.Diagnostics, "Invalid ETCD backup S3 config", err)

		return
	}

	if err := r.data.state.Create(ctx, etcdBackupS3Config); err != nil {
		if cosistate.IsConflictError(err) {
			resp.Diagnostics.AddError(
				"ETCD backup S3 config already exists",
				"The etcd backup S3 configuration is an Omni-wide singleton and is already configured. "+
					"Import it instead: terraform import <address> "+omni.EtcdBackupS3ConfID+"\n\n"+err.Error(),
			)

			return
		}

		errToDiag(&resp.Diagnostics, "Failed to create ETCD backup S3 config", err)

		return
	}

	plan.ID = types.StringValue(omni.EtcdBackupS3ConfID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Read implements resource.Resource.
func (r *etcdBackupS3ConfigResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state etcdBackupS3ConfigResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	etcdBackupS3Config, err := safe.ReaderGetByID[*omni.EtcdBackupS3Conf](ctx, r.data.state, omni.EtcdBackupS3ConfID)
	if err != nil {
		if cosistate.IsNotFoundError(err) {
			resp.State.RemoveResource(ctx)

			return
		}

		errToDiag(&resp.Diagnostics, "Failed to read ETCD backup S3 config", err)

		return
	}

	r.etcdBackupS3ConfigToModel(etcdBackupS3Config, &state)

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update implements resource.Resource.
func (r *etcdBackupS3ConfigResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan etcdBackupS3ConfigResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)

	if resp.Diagnostics.HasError() {
		return
	}

	_, err := safe.StateUpdateWithConflicts(ctx, r.data.state, omni.NewEtcdBackupS3Conf().Metadata(),
		func(etcdBackupS3Conf *omni.EtcdBackupS3Conf) error {
			return r.applyEtcdBackupConfigModel(plan, etcdBackupS3Conf)
		})
	if err != nil {
		errToDiag(&resp.Diagnostics, "Failed to update ETCD backup S3 config", err)

		return
	}

	plan.ID = types.StringValue(omni.EtcdBackupS3ConfID)

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

// Delete implements resource.Resource.
func (r *etcdBackupS3ConfigResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state etcdBackupS3ConfigResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)

	if resp.Diagnostics.HasError() {
		return
	}

	etcdBackupS3Config := omni.NewEtcdBackupS3Conf()

	if err := r.data.state.TeardownAndDestroy(ctx, etcdBackupS3Config.Metadata()); err != nil {
		if cosistate.IsNotFoundError(err) {
			return
		}

		errToDiag(&resp.Diagnostics, "Failed to destroy ETCD backup S3 config", err)

		return
	}
}

// ImportState implements resource.ResourceWithImportState. The configuration is a singleton, so the
// import ID is ignored and the well-known resource ID is always used.
func (r *etcdBackupS3ConfigResource) ImportState(ctx context.Context, _ resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), omni.EtcdBackupS3ConfID)...)
}

// applyEtcdBackupConfigModel builds an EtcdBackupS3Conf resource spec from the model, validating the model first.
func (r *etcdBackupS3ConfigResource) applyEtcdBackupConfigModel(plan etcdBackupS3ConfigResourceModel, etcdBackupS3Config *omni.EtcdBackupS3Conf) error {
	bucket := strings.TrimSpace(plan.Bucket.ValueString())
	region := strings.TrimSpace(plan.Region.ValueString())

	if bucket == "" {
		return errors.New("bucket must not be empty")
	}

	accessKeyID := plan.AccessKeyID.ValueString()
	secretAccessKey := plan.SecretAccessKey.ValueString()
	sessionToken := plan.SessionToken.ValueString()

	// Static credentials are all-or-nothing: Omni rejects a half pair outright, and with both halves
	// unset it falls back to whatever credentials its own environment provides.
	if (accessKeyID == "") != (secretAccessKey == "") {
		return errors.New("access_key_id and secret_access_key must be set together")
	}

	// Omni only passes the session token to the static credentials provider, so a token without a
	// static key pair would be silently dropped.
	if accessKeyID == "" && sessionToken != "" {
		return errors.New("session_token requires access_key_id and secret_access_key to be set")
	}

	value := etcdBackupS3Config.TypedSpec().Value

	value.Bucket = bucket
	value.Region = region
	value.Endpoint = strings.TrimSpace(plan.Endpoint.ValueString())
	value.AccessKeyId = accessKeyID
	value.SecretAccessKey = secretAccessKey
	value.SessionToken = sessionToken

	return nil
}

// etcdBackupS3ConfigToModel populates the model from an EtcdBackupS3Conf resource read from Omni.
func (r *etcdBackupS3ConfigResource) etcdBackupS3ConfigToModel(etcdBackupS3Config *omni.EtcdBackupS3Conf, model *etcdBackupS3ConfigResourceModel) {
	value := etcdBackupS3Config.TypedSpec().Value

	model.ID = types.StringValue(etcdBackupS3Config.Metadata().ID())
	model.Bucket = types.StringValue(value.GetBucket())

	// The optional attributes are not Computed: the server reports the zero value for anything the
	// user left unset, and materializing "" into state would diff forever against a null config.
	model.Region = optionalString(value.GetRegion())
	model.Endpoint = optionalString(value.GetEndpoint())
	model.AccessKeyID = optionalString(value.GetAccessKeyId())
	model.SecretAccessKey = optionalString(value.GetSecretAccessKey())
	model.SessionToken = optionalString(value.GetSessionToken())
}
