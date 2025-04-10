## Developing

### Dependency

* Kubernetes cluster with at least 4 cpus and 8192Mi of ram free. Should be a recent enough version. Can be local or remote it doesn't matter. It should not be a production cluster, although it will create random namespaces to avoid clobbering anything in prod. NOTE: this means whenever you run tests it will be creating and delete resources on k8s
* 2 VMs or on Prem nodes with ssh access.

#### Steps to setup the SSH nodes
* setup 2 vms
* install dremio enterprise (version 24 ideally)
* setup ssh public key auth
* setup a pat
* setup this file ./integration_test/ssh/testdata/ssh.json with a template like this (only use real values for all the fields)
{   
    "sudo_user": "dremio",
    "user": "myuser", 
    "coordinator": "coordinator-ip",
    "executor": "executor1",
    "dremio-log-dir": "/opt/dremio/log",
    "dremio-conf-dir": "/opt/dremio/conf",
    "dremio-rocksdb-dir": "/opt/dremio/cm/db/",
    "dremio-username": "dremio",
    "dremio-pat": "mytoken",
    "dremio-endpoint": "http://localhost:9047",
    "is-enterprise": true
}



### Scripts

On Linux, Mac, and WSL there are some shell scripts modeled off the [GitHub ones](https://github.com/github/scripts-to-rule-them-all)

to get started run

```sh
./script/bootstrap
```

after a pull it is a good idea to run

```sh
./script/update
```

tests

```sh
./script/test
```

before checkin run

```sh
./script/cibuild
```

to cut a release do the following

```sh
#dont forget to update changelog.md with the release notes
git tag v0.1.1
git push origin v0.1.1
./script/release v0.1.1
```


