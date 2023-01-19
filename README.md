# billing-collector-cloudservices

[![Build](https://img.shields.io/github/workflow/status/vshn/billing-collector-cloudservices/Test)][build]
![Go version](https://img.shields.io/github/go-mod/go-version/vshn/billing-collector-cloudservices)
[![Version](https://img.shields.io/github/v/release/vshn/billing-collector-cloudservices)][releases]
[![GitHub downloads](https://img.shields.io/github/downloads/vshn/billing-collector-cloudservices/total)][releases]

[build]: https://github.com/vshn/billing-collector-cloudservices/actions?query=workflow%3ATest
[releases]: https://github.com/vshn/billing-collector-cloudservices/releases

Batch job to sync usage data from the Exoscale and Cloudscale API to the [APPUiO Cloud reporting](https://github.com/appuio/appuio-cloud-reporting/) database.

Metrics are collected taking into account product (e.g. `object-storage-storage:exoscale`), source (e.g. `exoscale:namespace`), tenant (as organization) and date time.

See the [component documentation](https://hub.syn.tools/billing-collector-cloudservices/index.html) for more information.

## Getting started for developers

In order to run this tool, you need
* An instance of the billing database
* Access to the Exoscale and Cloudscale accounts which has the services to be invoiced
* Access to the Kubernetes cluster which has the claims corresponding to the Exoscale services

Get all this (see below), and put it all into an 'env' file:

```
export EXOSCALE_API_KEY="..."
export EXOSCALE_API_SECRET="..."
export KUBERNETES_SERVER_URL='https://...'
export KUBERNETES_SERVER_TOKEN='...'
export ACR_DB_URL="postgres://reporting:reporting@localhost/appuio-cloud-reporting-test?sslmode=disable"
```

Then source the env file and run the client:

```
$ . ./env
$ make build
```

Then, run one of the available commands:

* Object Storage:
```
$ ./billing-collector-cloudservices exoscale objectstorage
```

* DBaaS (runs metrics collector for all supported databases):
```
$ ./billing-collector-cloudservices exoscale dbaas
```

### Billing Database

Provided that you have Docker installed, you can easily run a local instance of the billing database by getting the [appuio-cloud-reporting](https://github.com/appuio/appuio-cloud-reporting/) repository and running:

```
$ make start-acr
```

### Create Resources in Lab Cluster to test metrics collector

You can first connect to your cluster and then create a claim for Postgres Database by applying a claim, for example:

```
apiVersion: appcat.vshn.io/v1
kind: ExoscalePostgreSQL
metadata:
  namespace: default
  name: exoscale-postgres-lab-test-1
spec:
  parameters:
    backup:
      timeOfDay: '13:00:00'
    maintenance:
      dayOfWeek: monday
      timeOfDay: "12:00:00"
    size:
      plan: hobbyist-2
    service:
      majorVersion: "14"
  writeConnectionSecretToRef:
    name: postgres-connection-details
```

Once the database is created and `Ready`, you can run locally the command:
```
$ ./billing-collector-cloudservices exoscale dbaas
```

The same works for other resources. Just apply the right claim and run the proper command.

And don't forget to delete the resource(s) you created once you're done testing.

### Exoscale API key and secret

You can get your Exoscale API key and secret from the Exoscale web UI. Be sure to select the correct project.

The token should be restricted to the 'sos' and 'dbaas' services.

### Kubernetes API token

The following instructions work for OpenShift via the 'oc' utility. Not all of them will work with kubectl.

The commands assume that you are logged in to the Kubernetes cluster you want to use, and your working directory needs to be this git repository.

Instructions for OpenShift >=4.11:
```
$ cd billing-collector-cloudservices
$ oc -n default --as cluster-admin apply -f clusterrole.yaml 
$ oc -n default --as cluster-admin create serviceaccount vshn-billing-collector-cloudservices
$ oc --as cluster-admin adm policy add-cluster-role-to-user vshn-billing-collector-cloudservices system:serviceaccount:default:vshn-billing-collector-cloudservices
$ oc -n default --as cluster-admin apply -f clusterrole-secret.yaml
$ oc -n default --as cluster-admin get secret vshn-billing-collector-cloudservices-secret -o jsonpath='{.data.token}' | base64 -d
```

Instructions for OpenShift <=4.10:
```
$ cd billing-collector-cloudservices
$ oc -n default --as cluster-admin apply -f clusterrole.yaml 
$ oc -n default --as cluster-admin create serviceaccount vshn-billing-collector-cloudservices
$ oc --as cluster-admin adm policy add-cluster-role-to-user vshn-billing-collector-cloudservices system:serviceaccount:default:vshn-billing-collector-cloudservices
$ oc -n default --as cluster-admin serviceaccounts get-token vshn-billing-collector-cloudservices
```

The last command will print out your token without trailing newline; be sure to copy the correct part of the output.

### Integration tests

Integration tests create an envtest cluster and store data in an ACR (appuio-cloud-reporting) database. This is all automated when running:

```bash
$ make test-integration
```

To run integration tests in your IDE of choice, be sure to set build tag `integration` and the following env variables:

```bash
ACR_DB_URL=postgres://reporting:reporting@localhost/appuio-cloud-reporting-test?sslmode=disable
CLOUDSCALE_API_TOKEN=<REDACTED>
EXOSCALE_API_KEY=<REDACTED>
EXOSCALE_API_SECRET=<REDACTED>

# path to directory where the respective go modules are installed. You can also specify the path to the local clone of the respective repositories.
EXOSCALE_CRDS_PATH="$(go list -f '{{.Dir}}' -m github.com/vshn/provider-exoscale)/package/crds)"
CLOUDSCALE_CRDS_PATH="$(go list -f '{{.Dir}}' -m github.com/vshn/provider-cloudscale)/package/crds)"

# make sure to run make target `test-integration` first to have everything setup correctly.
KUBEBUILDER_ASSETS="$(/path/to/billing-collector-cloudservices/.work/bin/setup-envtest --bin-dir "/path/to/billing-collector-cloudservices/.work/bin" use -i -p path '1.24.x!')"
```
