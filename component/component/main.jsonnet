local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local params = inv.parameters.exoscale_metrics_collector;
local paramsACR = inv.parameters.appuio_cloud_reporting;
local kube = import 'lib/kube.libjsonnet';
local com = import 'lib/commodore.libjsonnet';
local collectorImage = '%(registry)s/%(repository)s:%(tag)s' % params.images.collector;
local alias = inv.parameters._instance;
local alias_suffix = '-' + alias;
local credentials_secret_name = 'credentials' + alias_suffix;
local component_name = 'exoscale-metrics-collector';


local labels = {
  'app.kubernetes.io/name': component_name,
  'app.kubernetes.io/managed-by': 'commodore',
  'app.kubernetes.io/part-of': 'appuio-cloud-reporting',
  'app.kubernetes.io/component': component_name,
};

local secrets = [
  if params.secrets[s] != null then
    kube.Secret(s + alias_suffix) {
      metadata+: {
        namespace: paramsACR.namespace,
      },
    } + com.makeMergeable(params.secrets[s])
  for s in std.objectFields(params.secrets)
];

{
  assert params.secrets != null : 'secrets must be set.',
  assert params.secrets.credentials != null : 'secrets.credentials must be set.',
  assert params.secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
  assert params.secrets.credentials.stringData.EXOSCALE_API_KEY != null : 'secrets.credentials.stringData.EXOSCALE_API_KEY must be set.',
  assert params.secrets.credentials.stringData.EXOSCALE_API_SECRET != null : 'secrets.credentials.stringData.EXOSCALE_API_SECRET must be set.',
  assert params.secrets.credentials.stringData.K8S_SERVER_URL != null : 'secrets.credentials.stringData.K8S_SERVER_URL must be set.',
  assert params.secrets.credentials.stringData.K8S_TOKEN != null : 'secrets.credentials.stringData.K8S_TOKEN must be set.',

  secrets: std.filter(function(it) it != null, secrets),

  cronjob: {
    kind: 'CronJob',
    apiVersion: 'batch/v1',
    metadata: {
      name: alias,
      namespace: paramsACR.namespace,
      labels+: labels,
    },
    spec: {
      concurrencyPolicy: 'Forbid',
      failedJobsHistoryLimit: 5,
      jobTemplate: {
        spec: {
          template: {
            spec: {
              restartPolicy: 'OnFailure',
              containers: [
                {
                  name: 'exoscale-metrics-collector-backfill',
                  image: collectorImage,
                  args: [
                    'objectstorage',
                  ],
                  envFrom: [
                    {
                      secretRef: {
                        name: credentials_secret_name
                      }
                    }
                  ],
                  env: [
                    {
                      name: 'password',
                      valueFrom: {
                        secretKeyRef: {
                          key: 'password',
                          name: 'reporting-db',
                        },
                      },
                    },
                    {
                      name: 'username',
                      valueFrom: {
                        secretKeyRef: {
                          key: 'username',
                          name: 'reporting-db',
                        },
                      },
                    },
                    {
                      name: 'ACR_DB_URL',
                      value: 'postgres://$(username):$(password)@%(host)s:%(port)s/%(name)s?%(parameters)s' % paramsACR.database,
                    },
                  ],
                  resources: {},
                },
              ],
            },
          },
        },
      },
      schedule: params.schedule,
      successfulJobsHistoryLimit: 3,
    },
  },
}
