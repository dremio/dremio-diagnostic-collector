pipeline {
    agent {
        kubernetes {
            agentInjection true
            defaultContainer 'agent'
            cloud 'kubernetes'
            yamlFile 'jenkins/agent.yaml'
        }
    }

    parameters {
        string(
            name: 'GCP_PROJECT_ID',
            defaultValue: 'your-gcp-project',
            description: 'GCP Project ID'
        )
        string(
            name: 'GCP_ZONE',
            defaultValue: 'us-west1-b',
            description: 'GCP Zone for VM instances'
        )
        string(
            name: 'GCP_NETWORK_SUBNET',
            defaultValue: 'primary-west',
            description: 'GCP Network Subnet name'
        )
        string(
            name: 'GCP_MACHINE_TYPE',
            defaultValue: 'e2-standard-16',
            description: 'GCE instance machine type'
        )
        string(
            name: 'GCP_DISK_SIZE',
            defaultValue: '100',
            description: 'Boot disk size in GB'
        )
        string(
            name: 'GCP_DISK_POLICY',
            defaultValue: '',
            description: 'Disk resource policy (optional, leave empty if not needed)'
        )
        string(
            name: 'GCP_IMAGE',
            defaultValue: 'projects/debian-cloud/global/images/debian-12-bookworm-v20240910',
            description: 'GCE boot disk image'
        )
        choice(
            name: 'CLEANUP_INSTANCES',
            choices: ['true', 'false'],
            description: 'Automatically delete GCE instances after build completes?'
        )
    }

    environment {
        // GCP Configuration - use parameters if provided, otherwise fall back to env vars
        GCP_PROJECT_ID = "${params.GCP_PROJECT_ID}"
        GCP_ZONE = "${params.GCP_ZONE}"
        GCP_NETWORK_SUBNET = "${params.GCP_NETWORK_SUBNET}"
        GCP_MACHINE_TYPE = "${params.GCP_MACHINE_TYPE}"
        GCP_DISK_SIZE = "${params.GCP_DISK_SIZE}"
        GCP_DISK_POLICY = "${params.GCP_DISK_POLICY}"
        GCP_IMAGE = "${params.GCP_IMAGE}"

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
                sh '''
                    apk add bash curl python3 py3-pip openssh-client

                    # Install gcloud SDK
                    curl -O https://dl.google.com/dl/cloudsdk/channels/rapid/downloads/google-cloud-sdk-458.0.1-linux-x86_64.tar.gz
                    tar -xzf google-cloud-sdk-458.0.1-linux-x86_64.tar.gz
                    ./google-cloud-sdk/install.sh --quiet --path-update=false

                    # Verify gcloud is installed
                    ./google-cloud-sdk/bin/gcloud version

                    # Generate SSH key for VM access
                    ssh-keygen -t ed25519 -f $HOME/.ssh/id_ed25519 -q -P ""
                '''
            }
        }

        stage('Install k3sup') {
            steps {
                sh '''
                    curl -O -L https://github.com/alexellis/k3sup/releases/download/${K3SUP_VERSION}/k3sup
                    chmod +x k3sup
                    mkdir -p $HOME/bin
                    mv k3sup $HOME/bin/
                '''
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
                        ./google-cloud-sdk/bin/gcloud compute instances create $node_name \\
                            --project=${GCP_PROJECT_ID} \\
                            --zone=${GCP_ZONE} \\
                            --machine-type=${GCP_MACHINE_TYPE} \\
                            --network-interface=network-tier=PREMIUM,stack-type=IPV4_ONLY,subnet=${GCP_NETWORK_SUBNET} \\
                            --maintenance-policy=MIGRATE \\
                            --provisioning-model=STANDARD \\
                            --metadata="ssh-keys=jenkins:${SSH_PUBLIC_KEY}" \\
                            --create-disk=auto-delete=yes,boot=yes,device-name=$node_name,${DISK_POLICY_PARAM}image=${GCP_IMAGE},mode=rw,size=${GCP_DISK_SIZE},type=pd-balanced \\
                            --no-shielded-secure-boot \\
                            --shielded-vtpm \\
                            --shielded-integrity-monitoring \\
                            --labels=goog-ec-src=vm_add-gcloud \\
                            --reservation-affinity=any &
                    done
                    wait
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
                            MASTER_IP=$(./google-cloud-sdk/bin/gcloud compute instances describe $node_name --zone=${GCP_ZONE} --format='get(networkInterfaces[0].networkIP)')
                            $HOME/bin/k3sup install --ip $MASTER_IP --user jenkins --ssh-key $HOME/.ssh/id_ed25519
                        else
                            IP=$(./google-cloud-sdk/bin/gcloud compute instances describe $node_name --zone=${GCP_ZONE} --format='get(networkInterfaces[0].networkIP)')
                            $HOME/bin/k3sup join --ip $IP --server-ip $MASTER_IP --user jenkins --ssh-key $HOME/.ssh/id_ed25519
                        fi
                    done

                    mkdir -p $HOME/.kube
                    mv kubeconfig $HOME/.kube/config
                '''
            }
        }

        stage('Install Build Tools') {
            steps {
                sh '''
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
                // Cleanup: Delete GCE instances if CLEANUP_INSTANCES is true
                if (params.CLEANUP_INSTANCES == 'true') {
                    echo "Cleanup enabled - deleting GCE instances..."
                    sh '''#!/bin/bash
                        echo "Starting cleanup of GCE instances..."

                        for n in {1..4}; do
                            node_name=k8s-ddc-ci-$n-$BUILD_NUMBER
                            echo "Deleting $node_name"
                            # Run deletion in background and capture results
                            (
                                if ./google-cloud-sdk/bin/gcloud compute instances delete "$node_name" \\
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
                    '''
                } else {
                    echo "Cleanup disabled - GCE instances will remain running"
                    echo "Instance names: k8s-ddc-ci-{1..4}-${BUILD_NUMBER}"
                    echo "To delete manually, run the delete.Jenkinsfile job"
                }
            }
        }
    }
}
