[![Go Report Card](https://goreportcard.com/badge/github.com/dremio/dremio-diagnostic-collector)](https://goreportcard.com/report/github.com/dremio/dremio-diagnostic-collector)
![Coverage Status](https://img.shields.io/badge/Code%20Coverage-67%25-orange)

collect logs of dremio for analysis


### Install

Download the [latest release binaries](https://github.com/dremio/dremio-diagnostic-collector/releases/latest):

1. unzip the binary
2. open a terminal
3. change to the directory where you unzip your binary
4. run the command `./ddc -h` if you get the help for the command you are good to go.


### ddc.yaml

The ddc.yaml file is located next to your ddc binary. The defaults for the ddc.yaml are optimized for k8s and the dremio helm charts. However, if you have a custom installation or are using this with SSH you will likely have to edit the the ddc.yaml file included. 

```yaml

# please set these to match your environment
dremio-log-dir: "/var/log/dremio" # where the dremio log is located
dremio-conf-dir: "/opt/dremio/conf/..data" #where the dremio conf files are located
dremio-rocksdb-dir: /opt/dremio/data/db # used for locating Dremio's KV Metastore

## these are optional
# dremio-endpoint: "http://localhost:9047" # dremio endpoint on each node to use for collecting Workload Manager, KV Report and Job Profiles
# dremio-username: "dremio" # dremio user to for collecting Workload Manager, KV Report and Job Profiles 
# dremio-pat-token: "" # when set will attempt to collect Workload Manager, KV report and Job Profiles. Dremio PATs can be enabled by the support key auth.personal-access-tokens.enabled
# dremio-gclogs-dir: "" # if left blank detection is used to find the gc log dir
# verbose: vv
# collect-acceleration-log: false
# collect-access-log: false
# collect-audit-log: false
# collect-dremio-configuration: true # will collect dremio.conf, dremio-env, logback.xml and logback-access.xml
# number-job-profiles: 25000 # up to this number, may have less due to duplicates NOTE: need to have the dremio-pat set to work
# capture-heap-dump: false # when true a heap dump will be captured on each node that the collector is run against
# accept-collection-consent: true # when true you accept consent to collect data on each node, if false collection will fail
# allow-insecure-ssl: true # when true skip the ssl cert check when doing API calls
# number-threads: 2 #number of threads to use for collection

## not typically recommended to change
# tmp-output-dir: "" # dynamically set normally
# disable-rest-api: false
# dremio-pid: 0
# collect-metrics: true
# collect-os-config: true
# collect-disk-usage: true
# dremio-logs-num-days: 7
# dremio-queries-json-num-days: 28
# dremio-gc-file-pattern: "gc*.log*"
# collect-queries-json: true
# collect-jvm-flags: true
# collect-server-logs: true
# collect-meta-refresh-log: true
# collect-reflection-log: true
# collect-gc-logs: true
# collect-jfr: true
# collect-jstack: true
# collect-system-tables-export: true
# system-tables-row-limit: 100000
# collect-wlm: true
# collect-kvstore-report: true
# dremio-jstack-time-seconds: 60
# dremio-jfr-time-seconds: 60
# node-metrics-collect-duration-seconds: 60
# dremio-jstack-freq-seconds: 1
# node-name: "" //dynamically set normally
# is-dremio-cloud: false
# dremio-cloud-project-id: ""
# allow-insecure-ssl: true
# job-profiles-num-high-query-cost: 5000 // dynamically set
# job-profiles-num-slow-exec: 10000 // dynamically set
# job-profiles-num-recent-errors: 5000 // dynamically set
# job-profiles-num-slow-planning: 5000 // dynamically set
# rest-http-timeout: 30
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

### dremio cloud (Preview)
Specify the following parameters in ddc.yaml
```is-dremio-cloud: true
dremio-endpoint: "[eu.]dremio.cloud"    # Specify whether EU Dremio Cloud or not
dremio-cloud-project-id: "<PROJECT_ID>"
dremio-pat-token: "<DREMIO_PAT>"
tmp-output-dir: /full/path/to/dir       # Specify local target directory
```
and run
```sh
./ddc local-collect
```

## What is collected?

As of the today the following is collected

### By default

* Linux perf metrics (io, cpu, bytes read and written, io wait, etc)
* System disk usage
* Java Flight Recorder recording of 60 seconds
* Jstack thread dump every second for approximately 60 seconds
* server.log and 7 days of archives
* metadata_refresh.log and 7 days of archives
* reflection.log and 7 days of archives
* queries.json and up to 28 days of archives 
* all dremio configurations
* All gc logs if present

### Optionally with the appropriate change to ddc.yaml

* access.log and 7 days of archives
* audit.log and 7 days of archvies
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
  completion    Generate the autocompletion script for the specified shell
  help          Help about any command
  local-collect retrieves all the dremio logs and diagnostics for the local node and saves the results in a compatible format for Dremio support
  version       Print the version number of DDC

Flags:
  -c, --coordinator string             coordinator to connect to for collection. With ssh set a list of ip addresses separated by commas. In K8s use a label that matches to the pod(s).
      --coordinator-container string   for use with -k8s flag: sets the container name to use to retrieve logs in the coordinators (default "dremio-master-coordinator")
  -t, --dremio-pat-prompt              Prompt for Dremio Personal Access Token (PAT)
  -e, --executors string               either a common separated list or a ip range of executors nodes to connect to. With ssh set a list of ip addresses separated by commas. In K8s use a label that ma.
      --executors-container string     for use with -k8s flag: sets the container name to use to retrieve logs in the executors (default "dremio-executor")
  -h, --help                           help for ddc
  -k, --k8s                            use kubernetes to retrieve the diagnostics instead of ssh, instead of hosts pass in labels to the --cordinator and --executors flags
  -p, --kubectl-path string            where to find kubectl (default "kubectl")
  -n, --namespace string               namespace to use for kubernetes pods (default "default")
  -s, --ssh-key string                 location of ssh key to use to login
  -u, --ssh-user string                user to use during ssh operations to login
  -b, --sudo-user string               if any diagnostcs commands need a sudo user (i.e. for jcmd)

Use "ddc [command] --help" for more information about a command.
```
