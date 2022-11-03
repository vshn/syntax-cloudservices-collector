# exoscale-metrics-collector

[![Build](https://img.shields.io/github/workflow/status/vshn/exoscale-metrics-collector/Test)][build]
![Go version](https://img.shields.io/github/go-mod/go-version/vshn/exoscale-metrics-collector)
[![Version](https://img.shields.io/github/v/release/vshn/exoscale-metrics-collector)][releases]
[![Maintainability](https://img.shields.io/codeclimate/maintainability/vshn/exoscale-metrics-collector)][codeclimate]
[![Coverage](https://img.shields.io/codeclimate/coverage/vshn/exoscale-metrics-collector)][codeclimate]
[![GitHub downloads](https://img.shields.io/github/downloads/vshn/exoscale-metrics-collector/total)][releases]

[build]: https://github.com/vshn/exoscale-metrics-collector/actions?query=workflow%3ATest
[releases]: https://github.com/vshn/exoscale-metrics-collector/releases
[codeclimate]: https://codeclimate.com/github/vshn/exoscale-metrics-collector

Batch job to sync usage data from the Exoscale API to the [APPUiO Cloud reporting](https://github.com/appuio/appuio-cloud-reporting/) database.

See the [component documentation](https://hub.syn.tools/exoscale-metrics-collector/index.html) for more information.


## Getting started for developers

In order to run this tool, you need
* An instance of the billing database
* Access to the Exoscale account which has the services to be invoiced
* Access to the Kubernetes cluster which has the claims corresponding to the Exoscale services

Get all this (see below), and put it all into an 'env' file:

```
export EXOSCALE_API_KEY="..."
export EXOSCALE_API_SECRET="..."
export K8S_SERVER_URL='https://...'
export K8S_TOKEN='...'
export ACR_DB_URL="postgres://reporting:reporting@localhost/appuio-cloud-reporting-test?sslmode=disable"
```

Then source the env file and run the client:

```
$ . ./env
$ make build
$ ./exoscale-metrics-collector objectstorage
```

### Billing Database

Provided that you have Docker installed, you can easily run a local instance of the billing database by getting the [appuio-cloud-reporting](https://github.com/appuio/appuio-cloud-reporting/) repository and running:

```
$ cd appuio-cloud-reporting
$ make docker-compose-up
```

### Exoscale API key and secret

You can get your Exoscale API key and secret from the Exoscale web UI. Be sure to select the correct project.

The token should be restricted to the 'sos' and 'dbaas' services.

### Kubernetes API token

The following instructions work for OpenShift via the 'oc' utility. Not all of them will work with kubectl.

The commands assume that you are logged in to the Kubernetes cluster you want to use, and your working directory needs to be this git repository.

Instructions for OpenShift >=4.11:
```
$ cd exoscale-metrics-collector
$ oc -n default --as cluster-admin apply -f clusterrole.yaml 
$ oc -n default --as cluster-admin create serviceaccount vshn-exoscale-metrics-collector
$ oc --as cluster-admin adm policy add-cluster-role-to-user vshn-exoscale-metrics-collector system:serviceaccount:default:vshn-exoscale-metrics-collector
$ oc -n default --as cluster-admin apply -f clusterrole-secret.yaml
$ oc -n default --as cluster-admin get secret vshn-exoscale-metrics-collector-secret -o jsonpath='{.data.token}' | base64 -d
```

Instuctions for OpenShift <=4.10:
```
$ cd exoscale-metrics-collector
$ oc -n default --as cluster-admin apply -f clusterrole.yaml 
$ oc -n default --as cluster-admin create serviceaccount vshn-exoscale-metrics-collector
$ oc --as cluster-admin adm policy add-cluster-role-to-user vshn-exoscale-metrics-collector system:serviceaccount:default:vshn-exoscale-metrics-collector
$ oc -n default --as cluster-admin serviceaccounts get-token vshn-exoscale-metrics-collector
```

The last command will print out your token without trailing newline; be sure to copy the correct part of the output.
