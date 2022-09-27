local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local params = inv.parameters.exoscale_metrics_collector;
local paramsACR = inv.parameters.appuio_cloud_reporting;
local kube = import 'lib/kube.libjsonnet';
local com = import 'lib/commodore.libjsonnet';
local collectorImage = '%(registry)s/%(repository)s:%(tag)s' % params.images.collector;


local labels = {
  'app.kubernetes.io/name': 'exoscale-metrics-collector',
  'app.kubernetes.io/managed-by': 'commodore',
  'app.kubernetes.io/part-of': 'appuio-cloud-reporting',
  'app.kubernetes.io/component': 'exoscale-metrics-collector',
};

local secrets = [
  if params.secrets[s] != null then
    kube.Secret(s) {
      metadata+: {
        namespace: paramsACR.namespace,
      },
    } + com.makeMergeable(params.secrets[s])
  for s in std.objectFields(params.secrets)
];

{
  assert params.secrets != null : 'secrets must be set.',
  assert params.secrets.exoscale != null : 'secrets.exoscale must be set.',
  assert params.secrets.exoscale.stringData != null : 'secrets.exoscale.stringData must be set.',
  assert params.secrets.exoscale.stringData.api_key != null : 'secrets.exoscale.stringData.api_key must be set.',
  assert params.secrets.exoscale.stringData.api_secret != null : 'secrets.exoscale.stringData.api_secret must be set.',

  secrets: std.filter(function(it) it != null, secrets),

  cronjob: {
    kind: 'CronJob',
    apiVersion: 'batch/v1',
    metadata: {
      name: 'exoscale-metrics-collector',
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
                    'exoscale-metrics-collector',
                  ],
                  command: [ 'sh', '-c' ],
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
                    {
                      name: 'EXOSCALE_API_KEY',
                      valueFrom: {
                        secretKeyRef: {
                          key: 'api_key',
                          name: 'exoscale',
                        },
                      },
                    },
                    {
                      name: 'EXOSCALE_API_SECRET',
                      valueFrom: {
                        secretKeyRef: {
                          key: 'api_secret',
                          name: 'exoscale',
                        },
                      },
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
