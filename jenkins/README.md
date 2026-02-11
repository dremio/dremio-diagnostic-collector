# Jenkins Pipeline Configuration

This directory contains Jenkins pipeline definitions for the dremio-diagnostic-collector project.

## Pipelines

- **build.Jenkinsfile** - Main build pipeline that creates a K3s cluster and runs tests
- **release.Jenkinsfile** - Release pipeline for publishing releases
- **delete.Jenkinsfile** - Cleanup pipeline
- **agent.yaml** - Kubernetes pod template for Jenkins agents

## Build Pipeline Configuration

The `build.Jenkinsfile` requires several environment variables to be configured in Jenkins. These can be set via:
- Jenkins job configuration (Environment Variables)
- HashiCorp Vault (recommended for sensitive data)
- Jenkins global properties

### Required Environment Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `GCP_PROJECT_ID` | Google Cloud Project ID | `your-gcp-project` |
| `GCP_ZONE` | GCP zone for VM instances | `us-west1-b` |
| `GCP_SERVICE_ACCOUNT` | GCP service account email | `your-sa@developer.gserviceaccount.com` |
| `GCP_NETWORK_SUBNET` | GCP subnet name | `primary-west` |
| `GCP_MACHINE_TYPE` | GCE instance machine type | `e2-standard-16` |
| `GCP_DISK_SIZE` | Boot disk size in GB | `100` |
| `GCP_DISK_POLICY` | (Optional) Disk resource policy | `projects/PROJECT/regions/REGION/resourcePolicies/POLICY` |
| `GCP_IMAGE` | GCE boot disk image | `projects/debian-cloud/global/images/debian-12-bookworm-v20240910` |

### Required Vault Secrets

The build pipeline requires Google Cloud service account credentials stored in HashiCorp Vault:

**Path:** `secret/support/private/gcloud-service-account`  
**Key:** `credentials-file`  
**Value:** Complete JSON content of the GCP service account key file

### How to Configure

#### Option 1: Using Jenkins Environment Variables

1. Go to your Jenkins job → Configure
2. Check "Prepare an environment for the run"
3. Add the environment variables listed above

#### Option 2: Using Vault (Recommended)

Store all sensitive configuration in Vault and update the Jenkinsfile to retrieve them using `withVault` blocks.

Example:
```groovy
withVault(vaultSecrets: [[
    path: 'secret/support/private/gcp-config', 
    secretValues: [
        [envVar: 'GCP_PROJECT_ID', vaultKey: 'project-id'],
        [envVar: 'GCP_SERVICE_ACCOUNT', vaultKey: 'service-account'],
        // ... other values
    ]
]]) {
    // Your build steps
}
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

The build pipeline **automatically cleans up** all GCE instances after the build completes, regardless of success or failure. This happens in the `post { always }` section to ensure:

- No VMs are left running (avoiding unnecessary costs)
- Cleanup happens even if the build fails
- All 4 instances are deleted in parallel for speed

The cleanup stage will show output like:
```
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

## Security Notes

- Never commit GCP credentials, project IDs, or service account emails directly to the repository
- Use environment variables or Vault for all sensitive configuration
- Rotate service account keys regularly
- Use least-privilege service accounts with only necessary permissions

