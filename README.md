[![Go Report Card](https://goreportcard.com/badge/github.com/dremio/dremio-diagnostic-collector/v3)](https://goreportcard.com/report/github.com/dremio/dremio-diagnostic-collector/v3)


Automated log and analytics collection for Dremio clusters

## IMPORTANT LINKS

* Read the [FAQ](FAQ.md) for common questions on setting up DDC
* Read the [ddc.yaml](default-ddc.yaml) for a full, detailed list of customizable collection parameters (optional)
* Read the [official Dremio Support page](https://support.dremio.com/hc/en-us/articles/15560006579739) for more details on the DDC architecture
* Read the [ddc help](README.md#ddc-usage)
* Read the [DDC Diagnostic Tarball Contents](docs/ddc-tarball.md) to know what is saved by a DDC tarball

### Install DDC on your local machine

Download the [latest release binary](https://github.com/dremio/dremio-diagnostic-collector/releases/latest):

1. Unzip the binary
2. Open a terminal and change to the directory where you unzipped your binary
3. Run the command `./ddc help`. If you see the DDC command help, you are good to go.

### Guided Collection

```bash
ddc
```
#### select transport
```bash
 ? select transport for file transfers: 
  ▸ kubernetes
    ssh
```

#### select namespace for k8s
```bash
✔ kubernetes
Use the arrow keys to navigate: ↓ ↑ → ← 
? The following k8s namespaces have dremio clusters. Select the one you want to collect from: 
  ▸ default
    ns1
```

#### select collection type
```bash
✔ kubernetes
✔ default
Use the arrow keys to navigate: ↓ ↑ → ← 
? Collection Type: light (2 days logs), standard (7 days logs + 30 days queries.json), standard+jstack (standard w jstack), health-check (needs PAT): 
  ▸ light
    standard
    standard+jstack
    health-check
```
#### enjoy progress
```bash
=================================
== Dremio Diagnostic Collector ==
=================================
Wed, 12 Mar 2025 14:20:54 CET

Version              : ddc v3.3.1-d5a0c02
Yaml                 : /opt/homebrew/Cellar/ddc/3.3.1/libexec/ddc.yaml
Log File             : /opt/homebrew/Cellar/ddc/3.3.1/libexec/ddc.log
Collection Type      : Kubectl - context used: default
Collections Enabled  : disk-usage,dremio-configuration,gc-logs,jfr,jvm-flags,meta-refresh-log,os-config,queries-json,reflection-log,server-logs,ttop,vacuum-log
Collections Disabled : acceleration-log,access-log,audit-log,job-profiles,jstack,kvstore-report,system-tables-export,wlm
Collection Mode      : STANDARD
Collection Args      : namespace: 'default', label selector: 'role=dremio-cluster-pod'
Dremio PAT Set       : No (disables Job Profiles, WLM, KV Store and System Table Reports use --collect health-check if you want these)
Autodetect Enabled   : Yes

-- status --
Transfers Complete   : 0/1
Collect Duration     : elapsed 17 seconds
Tarball              : 
Result               : 

-- Warnings --



Kubernetes:
-----------
Last file collected   : pod dremio-master-0 logs
files collected       : 23

Nodes:
------
1. node dremio-master-0 - elapsed 8 secs - status COPY DDC TO HOST 
```

### Scripting - Dremio on Kubernetes

DDC connects via SSH or the kubernetes API and collects a series of logs and files for Dremio, then puts those collected files in an archive

For Kubernetes deployments _(Relies on a kubernetes configuration file to be at $HOME/.kube/config or at $KUBECONFIG)_:

##### default collection
```bash
ddc --namespace mynamespace
```
      
##### to collect job profiles, system tables, kv reports and wlm (via REST API)
_Requires Dremio admin privileges. Dremio PATs can be enabled by the support key `auth.personal-access-tokens.enabled`_
```bash
ddc  -n mynamespace  --collect health-check
```

### Scripting - Dremio on-prem

Specify executors that you want include in diagnostic collection with the `-e` flag and coordinators with the `-c` flag. Specify SSH user, and SSH key to use.

For SSH based communication to VMs or Bare Metal hardware:

##### coordinator only

```bash
ddc --coordinator 10.0.0.19 --ssh-user myuser 
```    
##### coordinator and executors
        
```bash
ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser
```

##### to collect job profiles, system tables, kv reports and wlm (via REST API)
_Requires Dremio admin privileges. Dremio PATs can be enabled by the support key `auth.personal-access-tokens.enabled`_
```bash
ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --sudo-user dremio --ssh-user myuser --collect health-check
```    
    
##### to avoid using the /tmp folder on nodes

```bash
ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --sudo-user dremio --ssh-user myuser --transfer-dir /mnt/lots_of_storage/
```

### Dremio AWSE

Log-only collection from a Dremio AWSE coordinator is possible via the following command. This will produce a tarball with logs from all nodes.

```bash
./ddc awselogs
```

### REST-API-only collection (works for all Dremio Software variants)
To collect job profiles, system tables, and wlm via REST API, specify the following parameters in `ddc.yaml`
```yaml
is-rest-collect: true
rest-collect-daily-jobs-limit: 100000 # Optional; Used to prevent reading from large sys.jobs_recent table
dremio-endpoint: "<DREMIO_ENDPOINT>"
dremio-pat-token: "<DREMIO_PAT>"
tarball-out-dir: /full/path/to/local/dir  # Specify local target directory
```
and run `./ddc local-collect` from your local machine

### Dremio Cloud
To collect job profiles, system tables, and wlm via REST API, specify the following parameters in `ddc.yaml`
```yaml
is-dremio-cloud: true
dremio-endpoint: "[eu.]dremio.cloud"    # Specify whether EU Dremio Cloud or not
dremio-cloud-project-id: "<PROJECT_ID>"
dremio-pat-token: "<DREMIO_PAT>"
tarball-out-dir: /full/path/to/dir      # Specify local target directory
```
and run `./ddc local-collect` from your local machine

### Windows Users

If you are running DDC from Windows, always run in a shell from the `C:` drive prompt. 
This is because of a limitation of kubectl ( see https://github.com/kubernetes/kubernetes/issues/77310 )

### ddc.yaml

The `ddc.yaml` file is located next to your DDC binary and can be edited to fit your environment. The [default-ddc.yaml](default-ddc.yaml) documents the full list of available parameters.


### ddc usage

```bash
ddc v3.2.3-79ea60d
 ddc connects via ssh or kubectl and collects a series of logs and files for dremio, then puts those collected files in an archive
examples:

for a ui prompt just run:
	ddc 

for ssh based communication to VMs or Bare metal hardware:

	ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser --ssh-key ~/.ssh/mykey --sudo-user dremio 

for kubernetes deployments:

	# run against a specific namespace and retrieve 2 days of logs
	ddc --namespace mynamespace

	# run against a specific namespace with a standard collection (includes jfr, top and 30 days of queries.json logs)
	ddc --namespace mynamespace	--collect standard

	# run against a specific namespace with a Health Check (runs 2 threads and includes everything in a standard collection plus collect 25,000 job profiles, system tables, kv reports and Work Load Manager (WLM) reports)
	ddc --namespace mynamespace	--collect health-check

Usage:
  ddc [flags]
  ddc [command]

Available Commands:
  awselogs      Log only collect of AWSE from the coordinator node
  completion    Generate the autocompletion script for the specified shell
  help          Help about any command
  local-collect retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support
  version       Print the version number of DDC

Flags:
      --collect string             type of collection: 'light'- 2 days of logs (no top or jfr). 'standard' - includes jfr, top, 7 days of logs and 30 days of queries.json logs. 'standard+jstack' - all of 'standard' plus jstack. 'health-check' - all of 'standard' + WLM, KV Store Report, 25,000 Job Profiles (default "light")
  -x, --context string             K8S ONLY: context to use for kubernetes pods
  -c, --coordinator string         SSH ONLY: set a list of ip addresses separated by commas
      --ddc-yaml string            location of ddc.yaml that will be transferred to remote nodes for collection configuration (default "/opt/homebrew/Cellar/ddc/3.2.3/libexec/ddc.yaml")
      --detect-namespace           detect namespace feature to pass the namespace automatically
      --disable-free-space-check   disables the free space check for the --transfer-dir
  -d, --disable-kubectl            uses the embedded k8s api client and skips the use of kubectl for transfers and copying
      --disable-prompt             disables the prompt ui
  -e, --executors string           SSH ONLY: set a list of ip addresses separated by commas
  -h, --help                       help for ddc
  -l, --label-selector string      K8S ONLY: select which pods to collect: follows kubernetes label syntax see https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/#label-selectors (default "role=dremio-cluster-pod")
      --min-free-space-gb int      min free space needed in GB for the process to run (default 40)
  -n, --namespace string           K8S ONLY: namespace to use for kubernetes pods
      --output-file string         name and location of diagnostic tarball (default "diag.tgz")
  -t, --pat-prompt                 prompt for the pat, which will enable collection of kv report, system tables, job profiles and the workload manager report
  -s, --ssh-key string             SSH ONLY: of ssh key to use to login
  -u, --ssh-user string            SSH ONLY: user to use during ssh operations to login
  -b, --sudo-user string           SSH ONLY: if any diagnostics commands need a sudo user (i.e. for jcmd)
      --transfer-dir string        directory to use for communication between the local-collect command and this one (default "/tmp/ddc-20240906174311")
      --transfer-threads int       number of threads to transfer tarballs (default 2)

Use "ddc [command] --help" for more information about a command.

```
