# Jenkins Pipeline Configuration

This directory contains Jenkins pipeline definitions for the dremio-diagnostic-collector project.

## Pipelines

- **build.Jenkinsfile** - Main build pipeline that creates a K3s cluster and runs tests
- **release.Jenkinsfile** - Release pipeline for publishing releases
- **delete.Jenkinsfile** - Cleanup pipeline
- **agent.yaml** - Kubernetes pod template for Jenkins agents

## Build Pipeline Configuration

The `build.Jenkinsfile` uses **build parameters** that appear in the Jenkins UI when you click "Build with Parameters".

### Build Parameters

After the first build, Jenkins will show a "Build with Parameters" button with the following options:

| Parameter | Description | Default Value |
|-----------|-------------|---------------|
| `GCP_PROJECT_ID` | Google Cloud Project ID | `your-gcp-project` |
| `GCP_ZONE` | GCP zone for VM instances | `us-west1-b` |
| `GCP_SERVICE_ACCOUNT` | GCP service account email | `your-sa@developer.gserviceaccount.com` |
| `GCP_NETWORK_SUBNET` | GCP subnet name | `primary-west` |
| `GCP_MACHINE_TYPE` | GCE instance machine type | `e2-standard-16` |
| `GCP_DISK_SIZE` | Boot disk size in GB | `100` |
| `GCP_DISK_POLICY` | (Optional) Disk resource policy | _(empty)_ |
| `GCP_IMAGE` | GCE boot disk image | `projects/debian-cloud/global/images/debian-12-bookworm-v20240910` |
| `CLEANUP_INSTANCES` | Delete instances after build? | `true` |

**Note:** On the first build, click "Build Now" to let Jenkins discover the parameters. After that, you'll see "Build with Parameters" instead.

### Required Vault Secrets

The build pipeline requires Google Cloud service account credentials stored in HashiCorp Vault:

**Path:** `secret/support/private/gcloud-service-account`  
**Key:** `credentials-file`  
**Value:** Complete JSON content of the GCP service account key file

### How to Use

1. **First Build**: Click "Build Now" - Jenkins will scan the Jenkinsfile and discover the parameters
2. **Subsequent Builds**: Click "Build with Parameters" - You'll see a form with all the parameters
3. **Fill in your values** (or use the defaults)
4. **Click Build**

**Example values for Dremio:**
```
GCP_PROJECT_ID: dremio-1093
GCP_ZONE: us-west1-b
GCP_SERVICE_ACCOUNT: 73420150722-compute@developer.gserviceaccount.com
GCP_NETWORK_SUBNET: primary-west
GCP_DISK_POLICY: projects/dremio-1093/regions/us-west1/resourcePolicies/regression-spark3hive
CLEANUP_INSTANCES: true
```

## Pipeline Stages

The build pipeline consists of the following stages:

1. **Setup** - Install basic dependencies (bash, curl, gcloud SDK)
2. **Install k3sup** - Download and install k3sup tool for K3s cluster management
3. **GCloud Auth & SSH Setup** - Authenticate with GCP and generate SSH keys
4. **Create GCE Instances** - Spin up 4 GCE instances in parallel
5. **Setup K3s Cluster** - Install K3s master node and join worker nodes
6. **Install Build Tools** - Download Go and kubectl
7. **Build** - Execute the actual build via `./script/cibuild`

## Automatic Cleanup

The build pipeline can **automatically clean up** all GCE instances after the build completes. This is controlled by the `CLEANUP_INSTANCES` parameter:

- **`CLEANUP_INSTANCES=true`** (default): Deletes all 4 VMs after build completes (success or failure)
- **`CLEANUP_INSTANCES=false`**: Leaves VMs running for debugging/investigation

### When cleanup is enabled:

- No VMs are left running (avoiding unnecessary costs)
- Cleanup happens even if the build fails
- All 4 instances are deleted in parallel for speed

The cleanup stage will show output like:
```
Cleanup enabled - deleting GCE instances...
Starting cleanup of GCE instances...
Deleting k8s-ddc-ci-1-123
Deleting k8s-ddc-ci-2-123
Deleting k8s-ddc-ci-3-123
Deleting k8s-ddc-ci-4-123
  ✓ Successfully deleted k8s-ddc-ci-1-123
  ✓ Successfully deleted k8s-ddc-ci-2-123
  ✓ Successfully deleted k8s-ddc-ci-3-123
  ✓ Successfully deleted k8s-ddc-ci-4-123
Deletion complete
```

### When cleanup is disabled:

```
Cleanup disabled - GCE instances will remain running
Instance names: k8s-ddc-ci-{1..4}-123
To delete manually, run the delete.Jenkinsfile job
```

## Security Notes

- Never commit GCP credentials, project IDs, or service account emails directly to the repository
- Use environment variables or Vault for all sensitive configuration
- Rotate service account keys regularly
- Use least-privilege service accounts with only necessary permissions

