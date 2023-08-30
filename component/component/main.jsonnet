local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local params = inv.parameters.billing_collector_cloudservices;
local kube = import 'lib/kube.libjsonnet';
local com = import 'lib/commodore.libjsonnet';
local collectorImage = '%(registry)s/%(repository)s:%(tag)s' % params.images.collector;
local alias = inv.parameters._instance;
local alias_suffix = '-' + alias;
local credentials_secret_name = 'credentials' + alias_suffix;
local component_name = 'billing-collector-cloudservices';

assert std.member(inv.applications, 'appuio-cloud-reporting') : 'Component appuio-cloud-reporting must be installed';

local labels = {
  'app.kubernetes.io/name': component_name,
  'app.kubernetes.io/managed-by': 'commodore',
  'app.kubernetes.io/part-of': 'appuio-cloud-reporting',
  'app.kubernetes.io/component': component_name,
};

local secret(key) = [
  if params.secrets[key][s] != null then
    kube.Secret(s + alias_suffix) {
      metadata+: {
        namespace: params.namespace,
      },
    } + com.makeMergeable(params.secrets[key][s])
  for s in std.objectFields(params.secrets[key])
];

local dbEnv = [
  {
    name: name,
    valueFrom: {
      secretKeyRef: {
        name: params.cloud_reporting_dbsecret_name,
        key: name,
      },
    },
  }
  for name in std.objectFields(params.database_secret)
] + [
  {
    name: name,
    [if std.type(params.database_env[name]) == 'string' then 'value' else 'valueFrom']: params.database_env[name],
  }
  for name in std.objectFields(params.database_env)
] + [
  assert params.database.url != null : 'database.url must be set.';
  {
    name: 'DB_PARAMS',
    value: params.database.parameters,
  },
  {
    name: 'ACR_DB_URL',
    value: params.database.url,
  },
];

local cronjob(name, args, schedule) = {
  kind: 'CronJob',
  apiVersion: 'batch/v1',
  metadata: {
    name: name,
    namespace: params.namespace,
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
                name: 'billing-collector-cloudservices-backfill',
                image: collectorImage,
                args: args,
                envFrom: [
                  {
                    secretRef: {
                      name: credentials_secret_name,
                    },
                  },
                ],
                env: dbEnv,
                resources: {},
                [if std.length(params.extra_volumes) > 0 then 'volumeMounts']: [
                  { name: name } + params.extra_volumes[name].mount_spec
                  for name in std.objectFields(params.extra_volumes)
                ],
              },
            ],
            [if std.length(params.extra_volumes) > 0 then 'volumes']: [
              { name: name } + params.extra_volumes[name].volume_spec
              for name in std.objectFields(params.extra_volumes)
            ],
          },
        },
      },
    },
    schedule: schedule,
    successfulJobsHistoryLimit: 3,
  },
};

assert params.exoscale.enabled != params.cloudscale.enabled : 'only one of the components can be enabled: cloudscale or exoscale. not both and not neither.';

(if params.exoscale.enabled then {
   local secrets = params.secrets.exoscale,
   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_KEY != null : 'secrets.credentials.stringData.EXOSCALE_API_KEY must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_SECRET != null : 'secrets.credentials.stringData.EXOSCALE_API_SECRET must be set.',
   assert secrets.credentials.stringData.KUBERNETES_SERVER_URL != null : 'secrets.credentials.stringData.KUBERNETES_SERVER_URL must be set.',
   assert secrets.credentials.stringData.KUBERNETES_SERVER_TOKEN != null : 'secrets.credentials.stringData.KUBERNETES_SERVER_TOKEN must be set.',

   secrets: std.filter(function(it) it != null, secret('exoscale')),
   objectStorageCronjob: cronjob(alias + '-objectstorage', [ 'exoscale', 'objectstorage' ], params.exoscale.objectStorage.schedule),
   [if params.exoscale.dbaas.enabled then 'dbaasCronjob']: cronjob(alias + '-dbaas', [ 'exoscale', 'dbaas' ], params.exoscale.dbaas.schedule),
 } else {})
+
(if params.cloudscale.enabled then {
   local secrets = params.secrets.cloudscale,
   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.CLOUDSCALE_API_TOKEN != null : 'secrets.credentials.stringData.CLOUDSCALE_API_TOKEN must be set.',
   assert secrets.credentials.stringData.KUBERNETES_SERVER_URL != null : 'secrets.credentials.stringData.KUBERNETES_SERVER_URL must be set.',
   assert secrets.credentials.stringData.KUBERNETES_SERVER_TOKEN != null : 'secrets.credentials.stringData.KUBERNETES_SERVER_TOKEN must be set.',

   secrets: std.filter(function(it) it != null, secret('cloudscale')),
   [if params.cloudscale.objectStorage.enabled then 'objectStorageCronjob']: cronjob(alias + '-objectstorage', [ 'cloudscale', 'objectstorage' ], params.cloudscale.objectStorage.schedule),
 } else {})
