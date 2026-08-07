// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni

import (
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/siderolabs/omni/client/api/omni/specs"
	"github.com/siderolabs/omni/client/pkg/omni/resources/omni"
)

func TestEtcdBackupConfigApplyModel(t *testing.T) {
	r := &etcdBackupS3ConfigResource{}

	for _, tc := range []struct {
		name        string
		model       etcdBackupS3ConfigResourceModel
		expected    *specs.EtcdBackupS3ConfSpec
		expectedErr string
	}{
		{
			name: "static credentials",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:          types.StringValue("backups"),
				Region:          types.StringValue("us-east-1"),
				Endpoint:        types.StringValue("https://s3.example.com"),
				AccessKeyID:     types.StringValue("key-id"),
				SecretAccessKey: types.StringValue("secret"),
				SessionToken:    types.StringValue("token"),
			},
			expected: &specs.EtcdBackupS3ConfSpec{
				Bucket:          "backups",
				Region:          "us-east-1",
				Endpoint:        "https://s3.example.com",
				AccessKeyId:     "key-id",
				SecretAccessKey: "secret",
				SessionToken:    "token",
			},
		},
		{
			name: "ambient credentials",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:          types.StringValue("backups"),
				Region:          types.StringValue("eu-central-1"),
				Endpoint:        types.StringNull(),
				AccessKeyID:     types.StringNull(),
				SecretAccessKey: types.StringNull(),
				SessionToken:    types.StringNull(),
			},
			expected: &specs.EtcdBackupS3ConfSpec{
				Bucket: "backups",
				Region: "eu-central-1",
			},
		},
		{
			name: "surrounding whitespace is trimmed",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:   types.StringValue("  backups\n"),
				Region:   types.StringValue(" us-east-1 "),
				Endpoint: types.StringValue(" https://s3.example.com "),
			},
			expected: &specs.EtcdBackupS3ConfSpec{
				Bucket:   "backups",
				Region:   "us-east-1",
				Endpoint: "https://s3.example.com",
			},
		},
		{
			name: "blank bucket is rejected",
			model: etcdBackupS3ConfigResourceModel{
				Bucket: types.StringValue("   "),
				Region: types.StringValue("us-east-1"),
			},
			expectedErr: "bucket must not be empty",
		},
		{
			// Omni applies the region only when it is set, so S3-compatible storage without regions is
			// configured by leaving it out.
			name: "region is optional",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:   types.StringValue("backups"),
				Region:   types.StringNull(),
				Endpoint: types.StringValue("https://s3.example.com"),
			},
			expected: &specs.EtcdBackupS3ConfSpec{
				Bucket:   "backups",
				Endpoint: "https://s3.example.com",
			},
		},
		{
			name: "access key without secret is rejected",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:      types.StringValue("backups"),
				Region:      types.StringValue("us-east-1"),
				AccessKeyID: types.StringValue("key-id"),
			},
			expectedErr: "access_key_id and secret_access_key must be set together",
		},
		{
			name: "secret without access key is rejected",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:          types.StringValue("backups"),
				Region:          types.StringValue("us-east-1"),
				SecretAccessKey: types.StringValue("secret"),
			},
			expectedErr: "access_key_id and secret_access_key must be set together",
		},
		{
			name: "session token without static credentials is rejected",
			model: etcdBackupS3ConfigResourceModel{
				Bucket:       types.StringValue("backups"),
				Region:       types.StringValue("us-east-1"),
				SessionToken: types.StringValue("token"),
			},
			expectedErr: "session_token requires access_key_id and secret_access_key to be set",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			conf := omni.NewEtcdBackupS3Conf()

			err := r.applyEtcdBackupConfigModel(tc.model, conf)

			if tc.expectedErr != "" {
				if err == nil {
					t.Fatalf("applyEtcdBackupConfigModel() = nil, want error %q", tc.expectedErr)
				}

				if err.Error() != tc.expectedErr {
					t.Fatalf("applyEtcdBackupConfigModel() error = %q, want %q", err.Error(), tc.expectedErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("applyEtcdBackupConfigModel() returned unexpected error: %v", err)
			}

			if got := conf.TypedSpec().Value; !got.EqualVT(tc.expected) {
				t.Fatalf("spec = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestEtcdBackupConfigToModel(t *testing.T) {
	r := &etcdBackupS3ConfigResource{}

	t.Run("all fields set", func(t *testing.T) {
		conf := omni.NewEtcdBackupS3Conf()
		conf.TypedSpec().Value = &specs.EtcdBackupS3ConfSpec{
			Bucket:          "backups",
			Region:          "us-east-1",
			Endpoint:        "https://s3.example.com",
			AccessKeyId:     "key-id",
			SecretAccessKey: "secret",
			SessionToken:    "token",
		}

		var model etcdBackupS3ConfigResourceModel

		r.etcdBackupS3ConfigToModel(conf, &model)

		expected := etcdBackupS3ConfigResourceModel{
			ID:              types.StringValue(omni.EtcdBackupS3ConfID),
			Bucket:          types.StringValue("backups"),
			Region:          types.StringValue("us-east-1"),
			Endpoint:        types.StringValue("https://s3.example.com"),
			AccessKeyID:     types.StringValue("key-id"),
			SecretAccessKey: types.StringValue("secret"),
			SessionToken:    types.StringValue("token"),
		}

		if model != expected {
			t.Fatalf("model = %+v, want %+v", model, expected)
		}
	})

	// Unset optional fields come back from the server as empty strings; they must map to null so the
	// state matches a configuration that omits them.
	t.Run("unset optional fields are null", func(t *testing.T) {
		conf := omni.NewEtcdBackupS3Conf()
		conf.TypedSpec().Value = &specs.EtcdBackupS3ConfSpec{
			Bucket: "backups",
		}

		model := etcdBackupS3ConfigResourceModel{
			Region:          types.StringValue("stale"),
			Endpoint:        types.StringValue("https://stale.example.com"),
			AccessKeyID:     types.StringValue("stale"),
			SecretAccessKey: types.StringValue("stale"),
			SessionToken:    types.StringValue("stale"),
		}

		r.etcdBackupS3ConfigToModel(conf, &model)

		for name, got := range map[string]types.String{
			"region":            model.Region,
			"endpoint":          model.Endpoint,
			"access_key_id":     model.AccessKeyID,
			"secret_access_key": model.SecretAccessKey,
			"session_token":     model.SessionToken,
		} {
			if !got.IsNull() {
				t.Fatalf("%s = %v, want null", name, got)
			}
		}
	})
}

// TestEtcdBackupConfigRoundTrip asserts that a model survives a write to the spec and a read back,
// which is what keeps `terraform plan` empty after an apply.
func TestEtcdBackupConfigRoundTrip(t *testing.T) {
	r := &etcdBackupS3ConfigResource{}

	for _, tc := range []struct {
		name string
		plan etcdBackupS3ConfigResourceModel
	}{
		{
			name: "static credentials",
			plan: etcdBackupS3ConfigResourceModel{
				ID:              types.StringValue(omni.EtcdBackupS3ConfID),
				Bucket:          types.StringValue("backups"),
				Region:          types.StringValue("us-east-1"),
				Endpoint:        types.StringValue("https://s3.example.com"),
				AccessKeyID:     types.StringValue("key-id"),
				SecretAccessKey: types.StringValue("secret"),
				SessionToken:    types.StringValue("token"),
			},
		},
		{
			name: "no static credentials",
			plan: etcdBackupS3ConfigResourceModel{
				ID:              types.StringValue(omni.EtcdBackupS3ConfID),
				Bucket:          types.StringValue("backups"),
				Region:          types.StringValue("us-east-1"),
				Endpoint:        types.StringNull(),
				AccessKeyID:     types.StringNull(),
				SecretAccessKey: types.StringNull(),
				SessionToken:    types.StringNull(),
			},
		},
		{
			name: "regionless endpoint",
			plan: etcdBackupS3ConfigResourceModel{
				ID:              types.StringValue(omni.EtcdBackupS3ConfID),
				Bucket:          types.StringValue("backups"),
				Region:          types.StringNull(),
				Endpoint:        types.StringValue("https://s3.example.com"),
				AccessKeyID:     types.StringNull(),
				SecretAccessKey: types.StringNull(),
				SessionToken:    types.StringNull(),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := tc.plan

			conf := omni.NewEtcdBackupS3Conf()

			if err := r.applyEtcdBackupConfigModel(plan, conf); err != nil {
				t.Fatalf("applyEtcdBackupConfigModel() returned unexpected error: %v", err)
			}

			var readBack etcdBackupS3ConfigResourceModel

			r.etcdBackupS3ConfigToModel(conf, &readBack)

			if readBack != plan {
				t.Fatalf("round trip = %+v, want %+v", readBack, plan)
			}
		})
	}
}
