[![Go Report Card](https://goreportcard.com/badge/github.com/dremio/dremio-diagnostic-collector)](https://goreportcard.com/report/github.com/dremio/dremio-diagnostic-collector)
![Coverage Status](https://img.shields.io/badge/Code_Coverage-71%25-yellow)


collect logs of Dremio for analysis

### Install

Download the [latest release binaries](https://github.com/dremio/dremio-diagnostic-collector/releases/latest):

1. unzip the binary
2. open a terminal
3. change to the directory where you unzip your binary
4. run the command `./ddc -h` if you get the help for the command you are good to go.


#### Install on a Dremio Docker Image and run a local collect 

If using the helm chart make sure you have `-Ddremio.log.path=/opt/dremio/data/log` set under extraStartParams see https://support.dremio.com/hc/en-us/articles/9972445087771-Tuning-and-Sizing-for-Dremio-in-Kubernetes

```bash
cd /tmp
curl -o ddc-linux-amd64 -L https://github.com/dremio/dremio-diagnostic-collector/releases/download/v0.7.4/ddc-linux-amd64
chmod +x ./ddc-linux-amd64
curl -o ddc.yaml -L https://github.com/dremio/dremio-diagnostic-collector/releases/download/v0.7.4/ddc.yaml
./ddc-linux-amd64 local-collect --tarball-out-dir /tmp
```
Read the last log line to see where the tarball went

```bash
INFO:  2023/11/27 10:11:59 local.go:389: Archive /tmp/ccfc31060d9a.tar.gz complete
```

## User Docs

Read the [Dremio Diagnostic Collector KB Article](https://support.dremio.com/hc/en-us/articles/15560006579739-Using-DDC-to-collect-files-for-Support-Tickets)

### ddc.yaml

The ddc.yaml file is located next to your ddc binary. For a default capture one does not need to edit this 

```yaml

# for offline nodes you will need to set dremio-log-dir, dremio-conf-fir and dremio-rocksdb-dir these to match your environment
#dremio-log-dir: "/var/log/dremio" # where the dremio log is located
#dremio-conf-dir: "/opt/dremio/conf" #where the dremio conf files are located
#dremio-rocksdb-dir: /opt/dremio/data/db # used for locating Dremio's KV Metastore

## optional
# dremio-endpoint: "http://localhost:9047" # dremio endpoint on each node to use for collecting Workload Manager, KV Report and Job Profiles
# dremio-username: "dremio" # dremio user to for collecting Workload Manager, KV Report and Job Profiles 
# dremio-pat-token: "" # when set will attempt to collect Workload Manager, KV report and Job Profiles. Dremio PATs can be enabled by the support key auth.personal-access-tokens.enabled
# dremio-logs-num-days: 7
# dremio-queries-json-num-days: 28
# number-job-profiles: 25000 # up to this number, may have less due to duplicates NOTE: need to have the dremio-pat set to work
# number-threads: 2 #number of threads to use for collection
```
After you have adjusted the yaml to your liking run ddc with either the k8s or on prem options

### dremio on k8s

Just need to specify the namespace and labels of the coordinators and the executors, next you can specify an output file with -o flag
.tgz, .zip, and .tar.gz are supported

```sh
./ddc -k -n default -e app=dremio-executor -c app=dremio-coordinator
```

If you have issues consult the [k8s docs](docs/k8s.md)

### dremio on prem

specific executors that you want to collect from with the -e flag and coordinators with the -c flag. Specify ssh user, and ssh key to use.

```sh
./ddc -e 192.168.1.12,192.168.1.13 -c 192.168.1.19,192.168.1.2  --ssh-user ubuntu --ssh-key ~/.ssh/id_rsa 
```
If you have issues consult the [ssh docs](docs/ssh.md)

### dremio on AWSE

If you want to do a log only collection of AWSE say from the coordinator the following command will produce a tarball with all the logs from each node

```sh
./ddc awselogs
```

### dremio cloud (Preview)
Specify the following parameters in ddc.yaml
```
is-dremio-cloud: true
dremio-endpoint: "[eu.]dremio.cloud"    # Specify whether EU Dremio Cloud or not
dremio-cloud-project-id: "<PROJECT_ID>"
dremio-pat-token: "<DREMIO_PAT>"
tmp-output-dir: /full/path/to/dir       # Specify local target directory
```
and run
```sh
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


### Full Help

The help is pretty straight forward and comes with examples, also do not forget to look at the ddc.yaml for all options.

```sh
ddc connects via ssh or kubectl and collects a series of logs and files for dremio, then puts those collected files in an archive
examples:
for ssh based communication to VMs or Bare metal hardware:

        ddc --coordinator 10.0.0.19 --executors 10.0.0.20,10.0.0.21,10.0.0.22 --ssh-user myuser

for kubernetes deployments:

        ddc --k8s --namespace mynamespace --coordinator app=dremio-coordinator --executors app=dremio-executor 

To sample job profiles and collect system tables information, kv reports, and Workload Manager Information add the --dremio-pat-prompt flag:

        ddc --k8s -n mynamespace -c app=dremio-coordinator -e app=dremio-executor --dremio-pat-prompt

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
  -c, --coordinator string             coordinator to connect to for collection. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).
      --coordinator-container string   for use with -k8s flag: sets the container name to use to retrieve logs in the coordinators (default "dremio-master-coordinator")
  -t, --dremio-pat-prompt              Prompt for Dremio Personal Access Token (PAT)
  -e, --executors string               either a common separated list or a ip range of executors nodes to connect to. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).
      --executors-container string     for use with -k8s flag: sets the container name to use to retrieve logs in the executors (default "dremio-executor")
  -h, --help                           help for ddc
  -k, --k8s                            use kubernetes to retrieve the diagnostics instead of ssh, instead of hosts pass in labels to the --cordinator and --executors flags
  -p, --kubectl-path string            where to find kubectl (default "kubectl")
  -n, --namespace string               namespace to use for kubernetes pods (default "default")
  -s, --ssh-key string                 location of ssh key to use to login
  -u, --ssh-user string                user to use during ssh operations to login
  -b, --sudo-user string               if any diagnostics commands need a sudo user (i.e. for jcmd)

Use "ddc [command] --help" for more information about a command.
```
