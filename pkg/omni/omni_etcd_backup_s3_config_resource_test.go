// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package omni_test

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"github.com/cosi-project/runtime/pkg/safe"
	cosistate "github.com/cosi-project/runtime/pkg/state"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
	omnires "github.com/siderolabs/omni/client/pkg/omni/resources/omni"

	"github.com/siderolabs/terraform-provider-omni/pkg/omni"
)

// The etcd backup S3 configuration is an Omni-wide singleton, so this test is not run in parallel
// with itself: a second instance would collide on the same resource ID.
func TestAccOmniEtcdBackupConfigResource(t *testing.T) {
	var (
		bucket          = envOrDefault("OMNI_TEST_S3_BUCKET", "tf-acc-etcd-backups")
		region          = envOrDefault("OMNI_TEST_S3_REGION", "us-east-1")
		endpoint        = envOrDefault("OMNI_TEST_S3_ENDPOINT", "http://s3:8333")
		accessKeyID     = envOrDefault("OMNI_TEST_S3_ACCESS_KEY_ID", "tfaccesskey")
		secretAccessKey = envOrDefault("OMNI_TEST_S3_SECRET_ACCESS_KEY", "tfsecretkey")
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckEtcdBackupConfigDestroy,
		Steps: []resource.TestStep{
			{ // create with static credentials
				Config: testAccEtcdBackupConfigConfig(bucket, region, endpoint, accessKeyID, secretAccessKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "id", omnires.EtcdBackupS3ConfID),
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "bucket", bucket),
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "region", region),
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "endpoint", endpoint),
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "access_key_id", accessKeyID),
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "secret_access_key", secretAccessKey),
					resource.TestCheckNoResourceAttr("omni_etcd_backup_s3_config.test", "session_token"),
					testAccCheckEtcdBackupConfig(bucket, region, endpoint),
					testAccCheckEtcdBackupStoreHealthy(),
				),
			},
			{ // the singleton ID is enough to import, whatever ID is passed
				ResourceName:                         "omni_etcd_backup_s3_config.test",
				ImportState:                          true,
				ImportStateId:                        omnires.EtcdBackupS3ConfID,
				ImportStateVerify:                    true,
				ImportStateVerifyIdentifierAttribute: "id",
			},
			{ // update the bucket in place
				Config: testAccEtcdBackupConfigConfig(bucket+"-updated", region, endpoint, accessKeyID, secretAccessKey),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("omni_etcd_backup_s3_config.test", "bucket", bucket+"-updated"),
					testAccCheckEtcdBackupConfig(bucket+"-updated", region, endpoint),
					testAccCheckEtcdBackupStoreHealthy(),
				),
			},
		},
	})
}

// TestAccOmniEtcdBackupConfigResourceUnreachableBucket asserts that Omni's own validation — it lists
// the bucket before accepting the configuration — surfaces as a resource error rather than a
// silently broken backup target.
func TestAccOmniEtcdBackupConfigResourceUnreachableBucket(t *testing.T) {
	var (
		region          = envOrDefault("OMNI_TEST_S3_REGION", "us-east-1")
		endpoint        = envOrDefault("OMNI_TEST_S3_ENDPOINT", "http://s3:8333")
		accessKeyID     = envOrDefault("OMNI_TEST_S3_ACCESS_KEY_ID", "tfaccesskey")
		secretAccessKey = envOrDefault("OMNI_TEST_S3_SECRET_ACCESS_KEY", "tfsecretkey")
	)

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		CheckDestroy:             testAccCheckEtcdBackupConfigDestroy,
		Steps: []resource.TestStep{
			{
				Config:      testAccEtcdBackupConfigConfig("tf-acc-does-not-exist", region, endpoint, accessKeyID, secretAccessKey),
				ExpectError: regexp.MustCompile("Failed to create ETCD backup S3 config"),
			},
		},
	})
}

// TestAccOmniEtcdBackupConfigResourceInvalidCredentials asserts that half a credential pair is
// rejected at plan time, before anything reaches Omni.
func TestAccOmniEtcdBackupConfigResourceInvalidCredentials(t *testing.T) {
	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: omni.TestAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: `
provider "omni" {
  insecure_skip_tls_verify = true
}

resource "omni_etcd_backup_s3_config" "test" {
  bucket        = "tf-acc-etcd-backups"
  region        = "us-east-1"
  access_key_id = "key-id"
}
`,
				ExpectError: regexp.MustCompile("Invalid Attribute Combination"),
			},
		},
	})
}

func testAccEtcdBackupConfigConfig(bucket, region, endpoint, accessKeyID, secretAccessKey string) string {
	credentials := ""
	if accessKeyID != "" {
		credentials = fmt.Sprintf(`
  access_key_id     = %q
  secret_access_key = %q
`, accessKeyID, secretAccessKey)
	}

	return fmt.Sprintf(`
provider "omni" {
  insecure_skip_tls_verify = true
}

resource "omni_etcd_backup_s3_config" "test" {
  bucket   = %q
  region   = %q
  endpoint = %q
%s}
`, bucket, region, endpoint, credentials)
}

// testAccCheckEtcdBackupConfig asserts, via the live Omni API, that the singleton configuration
// holds the expected values.
func testAccCheckEtcdBackupConfig(bucket, region, endpoint string) resource.TestCheckFunc {
	return func(*terraform.State) error {
		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		conf, err := safe.ReaderGetByID[*omnires.EtcdBackupS3Conf](context.Background(), client.Omni().State(), omnires.EtcdBackupS3ConfID)
		if err != nil {
			return fmt.Errorf("failed to read etcd backup S3 config: %w", err)
		}

		value := conf.TypedSpec().Value

		for _, check := range []struct {
			name string
			got  string
			want string
		}{
			{name: "bucket", got: value.GetBucket(), want: bucket},
			{name: "region", got: value.GetRegion(), want: region},
			{name: "endpoint", got: value.GetEndpoint(), want: endpoint},
		} {
			if check.got != check.want {
				return fmt.Errorf("unexpected %s: got %q, want %q", check.name, check.got, check.want)
			}
		}

		return nil
	}
}

// testAccCheckEtcdBackupStoreHealthy asserts that Omni's backup store picked the configuration up
// and connected to the bucket. The store is reconciled asynchronously from a watch, so the status is
// polled rather than read once.
func testAccCheckEtcdBackupStoreHealthy() resource.TestCheckFunc {
	return func(*terraform.State) error {
		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()

		var lastErr string

		for {
			// The status lives in the ephemeral namespace, so it is fetched by full metadata.
			status, err := safe.StateGet[*omnires.EtcdBackupStoreStatus](ctx, client.Omni().State(), omnires.NewEtcdBackupStoreStatus().Metadata())
			if err != nil && !cosistate.IsNotFoundError(err) {
				return fmt.Errorf("failed to read etcd backup store status: %w", err)
			}

			if err == nil {
				lastErr = status.TypedSpec().Value.GetConfigurationError()
				if lastErr == "" {
					return nil
				}
			}

			select {
			case <-ctx.Done():
				return fmt.Errorf("etcd backup store did not become healthy: %q", lastErr)
			case <-time.After(time.Second):
			}
		}
	}
}

// testAccCheckEtcdBackupConfigDestroy asserts, via the live Omni API, that the singleton
// configuration is gone.
func testAccCheckEtcdBackupConfigDestroy(s *terraform.State) error {
	for _, rs := range s.RootModule().Resources {
		if rs.Type != "omni_etcd_backup_s3_config" {
			continue
		}

		client, err := newTestClient()
		if err != nil {
			return err
		}
		defer client.Close() //nolint:errcheck

		_, err = safe.ReaderGetByID[*omnires.EtcdBackupS3Conf](context.Background(), client.Omni().State(), omnires.EtcdBackupS3ConfID)
		if err == nil {
			return fmt.Errorf("etcd backup S3 config %q still exists", omnires.EtcdBackupS3ConfID)
		}

		if !cosistate.IsNotFoundError(err) {
			return fmt.Errorf("unexpected error checking etcd backup S3 config: %w", err)
		}
	}

	return nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
