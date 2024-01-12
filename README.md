[![Go Report Card](https://goreportcard.com/badge/github.com/dremio/dremio-diagnostic-collector)](https://goreportcard.com/report/github.com/dremio/dremio-diagnostic-collector)
![Coverage Status](https://img.shields.io/badge/Code_Coverage-71%25-yellow)


collect logs of Dremio for analysis

## IMPORTANT LINKS

* Read the [FAQ.md](FAQ.md) for questions on making DDC work the way you want
* Read the [ddc.yaml](default-ddc.yaml) as it is well documented and contains all the custom functionality you may want to customer

### Install

Download the [latest release binaries](https://github.com/dremio/dremio-diagnostic-collector/releases/latest):

1. unzip the binary
2. open a terminal
3. change to the directory where you unzip your binary
4. run the command `./ddc -h` if you get the help for the command you are good to go.

## User Docs

Read the [Dremio Diagnostic Collector KB Article](https://support.dremio.com/hc/en-us/articles/15560006579739-Using-DDC-to-collect-files-for-Support-Tickets)

### dremio on k8s

ddc connects via ssh or kubectl and collects a series of logs and files for dremio, then puts those collected files in an archive

for kubernetes deployments:

#### coordinator only
```bash
ddc --k8s --namespace mynamespace --coordinator app=dremio-coordinator 
```
    
#### coordinator and executors
```bash
ddc --k8s --namespace mynamespace --coordinator app=dremio-coordinator --executors app=dremio-executor 
```
      
#### to collect job profiles, system tables, kv reports and wlm 

```bash
ddc --k8s -n mynamespace -c app=dremio-coordinator -e app=dremio-executor --dremio-pat-prompt
```

### dremio on prem

specific executors that you want to collect from with the -e flag and coordinators with the -c flag. Specify ssh user, and ssh key to use.


for ssh based communication to VMs or Bare metal hardware:

#### coordinator only

```bash
ddc --coordinator 10.0.0.19 --ssh-user myuser 
```    
#### coordinator and executors
        
```bash
ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser
```

#### to collect job profiles, system tables, kv reports and wlm 
```bash
ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser  --dremio-pat-prompt
```    
    
### to avoid using the /tmp folder on nodes

```bash
ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser --transfer-dir /mnt/lots_of_storage/
```

### dremio on AWSE

If you want to do a log only collection of AWSE say from the coordinator the following command will produce a tarball with all the logs from each node

```bash
./ddc awselogs
```

### dremio cloud (Preview)
Specify the following parameters in ddc.yaml
```yaml
is-dremio-cloud: true
dremio-endpoint: "[eu.]dremio.cloud"    # Specify whether EU Dremio Cloud or not
dremio-cloud-project-id: "<PROJECT_ID>"
dremio-pat-token: "<DREMIO_PAT>"
tmp-output-dir: /full/path/to/dir       # Specify local target directory
```
and run
```bash
./ddc local-collect
```

### Windows Users

If you are running ddc from windows, always run in a shell from the `C:` drive prompt. 
This is because of a limitation of kubectl ( see https://github.com/kubernetes/kubernetes/issues/77310 )

## What is collected?

As of the today the following is collected

### By default

* Perf metrics (cpu and GC usage by thread)
* System disk usage
* Java Flight Recorder recording of 60 seconds
* Jstack thread dump every second for approximately 60 seconds
* server.log and 7 days of archives
* metadata\_refresh.log and 7 days of archives
* reflection.log and 7 days of archives
* queries.json and up to 28 days of archives 
* all dremio configurations
* All gc logs if present

### Optionally with the appropriate change to ddc.yaml

* access.log and 7 days of archives
* audit.log and 7 days of archives
* java heap dump

### Optionally with a Dremio Personal Access Token

* a sampling of job profiles (note 25000 jobs can take 15 minutes to collect)
* dremio key value store report
* dremio work load manager details
* system tables and their details

### ddc.yaml

The ddc.yaml file is located next to your ddc binary, it is well documented and you should edit it to fit your environment.

### Flags


```sh
Available Commands:
  awselogs      Log only collect of AWSE from the coordinator node
  completion    Generate the autocompletion script for the specified shell
  help          Help about any command
  local-collect retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support
  version       Print the version number of DDC

Flags:
  -c, --coordinator string             coordinator to connect to for collection. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).
      --coordinator-container string   for use with -k8s flag: sets the container name to use to retrieve logs in the coordinators (default "dremio-master-coordinator,dremio-coordinator")
      --ddc-yaml string                location of ddc.yaml that will be transferred to remote nodes for collection configuration (default "/Users/ryan.svihla/Documents/GitHub/dremio-diagnostic-collector/bin/ddc.yaml")
  -t, --dremio-pat-prompt              Prompt for Dremio Personal Access Token (PAT)
  -e, --executors string               either a common separated list or a ip range of executors nodes to connect to. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).
      --executors-container string     for use with -k8s flag: sets the container name to use to retrieve logs in the executors (default "dremio-executor")
  -h, --help                           help for ddc
  -k, --k8s                            use kubernetes to retrieve the diagnostics instead of ssh, instead of hosts pass in labels to the --coordinator and --executors flags
  -p, --kubectl-path string            where to find kubectl (default "kubectl")
  -n, --namespace string               namespace to use for kubernetes pods (default "default")
      --output-file string             name of tgz file to save the diagnostic collection to (default "diag.tgz")
  -s, --ssh-key string                 location of ssh key to use to login
  -u, --ssh-user string                user to use during ssh operations to login
  -b, --sudo-user string               if any diagnostics commands need a sudo user (i.e. for jcmd)
      --transfer-dir string            directory to use for communication between the local-collect command and this one (default "/tmp/ddc-20240112141658")

```
