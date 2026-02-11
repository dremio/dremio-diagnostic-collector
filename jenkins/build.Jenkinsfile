pipeline {
    agent {
        kubernetes {
            agentInjection true
            defaultContainer 'agent'
            cloud 'kubernetes'
            yamlFile 'jenkins/agent.yaml'
        }
    }

    environment {
        // Set PATH to include our custom bin directories and gcloud
        PATH = "${env.PATH}:${env.WORKSPACE}/google-cloud-sdk/bin:${env.HOME}/go/bin:${env.HOME}/bin"

        // GCP Configuration - these should be set in Jenkins configuration or Vault
        GCP_PROJECT_ID = "${env.GCP_PROJECT_ID ?: 'your-gcp-project'}"
        GCP_ZONE = "${env.GCP_ZONE ?: 'us-west1-b'}"
        GCP_SERVICE_ACCOUNT = "${env.GCP_SERVICE_ACCOUNT ?: 'your-service-account@developer.gserviceaccount.com'}"
        GCP_NETWORK_SUBNET = "${env.GCP_NETWORK_SUBNET ?: 'primary-west'}"
        GCP_MACHINE_TYPE = "${env.GCP_MACHINE_TYPE ?: 'e2-standard-16'}"
        GCP_DISK_SIZE = "${env.GCP_DISK_SIZE ?: '100'}"
        GCP_DISK_POLICY = "${env.GCP_DISK_POLICY ?: ''}"
        GCP_IMAGE = "${env.GCP_IMAGE ?: 'projects/debian-cloud/global/images/debian-12-bookworm-v20240910'}"

        // K3sup version
        K3SUP_VERSION = "0.13.9"

        // Go and kubectl versions
        GO_VERSION = "1.24.3"
        KUBECTL_VERSION = "v1.32.0"
    }

    options {
        timeout(time: 60, unit: 'MINUTES')
    }

    stages {
        stage('Setup') {
            steps {
                sh '''#!/bin/bash
                    apk add bash curl python3 py3-pip

                    # Install gcloud SDK
                    curl -O https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-sdk-458.0.1-linux-x86_64.tar.gz
                    tar -xzf google-cloud-sdk-458.0.1-linux-x86_64.tar.gz
                    ./google-cloud-sdk/install.sh --quiet --path-update=true
                    export PATH=$PATH:$(pwd)/google-cloud-sdk/bin

                    # Verify gcloud is installed
                    gcloud version
                '''
            }
        }

        stage('Install k3sup') {
            steps {
                sh '''#!/bin/bash
                    curl -O -L https://github.com/alexellis/k3sup/releases/download/${K3SUP_VERSION}/k3sup
                    chmod +x k3sup
                    mkdir -p $HOME/bin
                    mv k3sup $HOME/bin/
                '''
            }
        }

        stage('GCloud Auth & SSH Setup') {
            steps {
                withVault(vaultSecrets: [[
                    path: 'secret/support/private/gcloud-service-account',
                    secretValues: [
                        [envVar: 'GOOGLE_APPLICATION_CREDENTIALS_JSON', vaultKey: 'credentials-file'],
                    ]
                ]]) {
                    sh '''#!/bin/bash
                        # Write the credentials to a temporary file
                        echo "${GOOGLE_APPLICATION_CREDENTIALS_JSON}" > /tmp/gcloud-key.json

                        gcloud auth activate-service-account --key-file /tmp/gcloud-key.json
                        ssh-keygen -t ed25519 -f $HOME/.ssh/id_ed25519 -q -P ""

                        # Clean up the temporary file
                        rm -f /tmp/gcloud-key.json
                    '''
                }
            }
        }

        stage('Create GCE Instances') {
            steps {
                sh '''#!/bin/bash
                    # Function to find and read SSH public key
                    get_ssh_public_key() {
                        local ssh_dir="$HOME/.ssh"
                        local public_key=""

                        # Look for common SSH public key files in order of preference
                        for key_file in "id_ed25519.pub" "id_rsa.pub" "id_ecdsa.pub" "id_dsa.pub"; do
                            if [ -f "$ssh_dir/$key_file" ]; then
                                public_key=$(cat "$ssh_dir/$key_file" | tr -d '\\n\\r')
                                echo "Found SSH public key: $ssh_dir/$key_file" >&2
                                break
                            fi
                        done

                        if [ -z "$public_key" ]; then
                            echo "Error: No SSH public key found in $ssh_dir" >&2
                            echo "Please ensure you have one of the following files:" >&2
                            echo "  - $ssh_dir/id_ed25519.pub" >&2
                            echo "  - $ssh_dir/id_rsa.pub" >&2
                            echo "  - $ssh_dir/id_ecdsa.pub" >&2
                            echo "  - $ssh_dir/id_dsa.pub" >&2
                            exit 1
                        fi

                        echo "$public_key"
                    }

                    SSH_PUBLIC_KEY="$(get_ssh_public_key)"

                    # Build disk policy parameter if set
                    DISK_POLICY_PARAM=""
                    if [ -n "${GCP_DISK_POLICY}" ]; then
                        DISK_POLICY_PARAM="disk-resource-policy=${GCP_DISK_POLICY},"
                    fi

                    for n in {1..4}; do
                        node_name=k8s-ddc-ci-$n-$BUILD_NUMBER
                        gcloud compute instances create $node_name \\
                            --project=${GCP_PROJECT_ID} \\
                            --zone=${GCP_ZONE} \\
                            --machine-type=${GCP_MACHINE_TYPE} \\
                            --network-interface=network-tier=PREMIUM,stack-type=IPV4_ONLY,subnet=${GCP_NETWORK_SUBNET} \\
                            --maintenance-policy=MIGRATE \\
                            --provisioning-model=STANDARD \\
                            --metadata="ssh-keys=jenkins:${SSH_PUBLIC_KEY}" \\
                            --service-account=${GCP_SERVICE_ACCOUNT} \\
                            --scopes=https://www.googleapis.com/auth/devstorage.read_only,https://www.googleapis.com/auth/logging.write,https://www.googleapis.com/auth/monitoring.write,https://www.googleapis.com/auth/service.management.readonly,https://www.googleapis.com/auth/servicecontrol,https://www.googleapis.com/auth/trace.append \\
                            --create-disk=auto-delete=yes,boot=yes,device-name=$node_name,${DISK_POLICY_PARAM}image=${GCP_IMAGE},mode=rw,size=${GCP_DISK_SIZE},type=pd-balanced \\
                            --no-shielded-secure-boot \\
                            --shielded-vtpm \\
                            --shielded-integrity-monitoring \\
                            --labels=goog-ec-src=vm_add-gcloud \\
                            --reservation-affinity=any &
                    done
                    wait < <( jobs -p )
                    sleep 60
                '''
            }
        }

        stage('Setup K3s Cluster') {
            steps {
                sh '''#!/bin/bash
                    for n in {1..4}; do
                        node_name=k8s-ddc-ci-$n-$BUILD_NUMBER
                        if [ "$n" -eq 1 ]; then
                            MASTER_IP=$(gcloud compute instances describe $node_name --zone=${GCP_ZONE} --format='get(networkInterfaces[0].networkIP)')
                            k3sup install --ip $MASTER_IP --user jenkins --ssh-key $HOME/.ssh/id_ed25519
                        else
                            IP=$(gcloud compute instances describe $node_name --zone=${GCP_ZONE} --format='get(networkInterfaces[0].networkIP)')
                            k3sup join --ip $IP --server-ip $MASTER_IP --user jenkins --ssh-key $HOME/.ssh/id_ed25519
                        fi
                    done

                    mkdir -p $HOME/.kube
                    mv kubeconfig $HOME/.kube/config
                '''
            }
        }

        stage('Install Build Tools') {
            steps {
                sh '''#!/bin/bash
                    wget https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz
                    tar -C ../ -xzf go${GO_VERSION}.linux-amd64.tar.gz
                    curl -LO https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl
                    chmod +x kubectl
                    mv kubectl $HOME/bin
                '''
            }
        }

        stage('Build') {
            environment {
                KUBECONFIG = "${env.HOME}/.kube/config"
            }
            steps {
                sh './script/cibuild'
            }
        }
    }

    post {
        always {
            script {
                // Cleanup: Delete GCE instances
                // This runs whether the build succeeds or fails to avoid leaving VMs running
                withVault(vaultSecrets: [[
                    path: 'secret/support/private/gcloud-service-account',
                    secretValues: [
                        [envVar: 'GOOGLE_APPLICATION_CREDENTIALS_JSON', vaultKey: 'credentials-file'],
                    ]
                ]]) {
                    sh '''#!/bin/bash
                        # Write the credentials to a temporary file
                        echo "${GOOGLE_APPLICATION_CREDENTIALS_JSON}" > /tmp/gcloud-key.json

                        gcloud auth activate-service-account --key-file /tmp/gcloud-key.json

                        echo "Starting cleanup of GCE instances..."

                        for n in {1..4}; do
                            node_name=k8s-ddc-ci-$n-$BUILD_NUMBER
                            echo "Deleting $node_name"
                            # Run deletion in background and capture results
                            (
                                if gcloud compute instances delete "$node_name" \\
                                    --project=${GCP_PROJECT_ID} \\
                                    --zone=${GCP_ZONE} \\
                                    --quiet 2>/dev/null; then
                                    echo "  ✓ Successfully deleted $node_name"
                                else
                                    echo "  ✗ Failed to delete $node_name (may not exist)"
                                fi
                            ) &
                        done
                        wait
                        echo "Deletion complete"

                        # Clean up the temporary file
                        rm -f /tmp/gcloud-key.json
                    '''
                }
            }
        }
    }
}
