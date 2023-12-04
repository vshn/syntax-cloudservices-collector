local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local params = inv.parameters.billing_collector_cloudservices;
local kube = import 'lib/kube.libjsonnet';
local com = import 'lib/commodore.libjsonnet';
local collectorImage = '%(registry)s/%(repository)s:%(tag)s' % params.images.collector;
local component_name = 'billing-collector-cloudservices';

local labels = {
  'app.kubernetes.io/name': component_name,
  'app.kubernetes.io/managed-by': 'commodore',
  'app.kubernetes.io/part-of': 'appuio-cloud-reporting',
  'app.kubernetes.io/component': component_name,
};

local secret(key, suf) = [
  if params.secrets[key][s] != null then
    kube.Secret(s + '-' + key + if suf != '' then '-' + suf else '') {
      metadata+: {
        namespace: params.namespace,
      },
    } + com.makeMergeable(params.secrets[key][s]) + com.makeMergeable(params.secrets['odoo'][s])
  for s in std.objectFields(params.secrets[key])
];

local healthProbe = {
  httpGet: {
    path: '/metrics',
    port: 9123,
  },
  periodSeconds: 30,
};

local exoDbaasClusterRole = kube.ClusterRole('appcat:cloudcollector:exoscale:dbaas') + {
  rules: [
    {
      apiGroups: [ '*' ],
      resources: [ 'namespaces' ],
      verbs: [ 'get', 'list' ],
    },
    {
      apiGroups: [ 'exoscale.crossplane.io' ],
      resources: [
        'postgresqls',
        'mysqls',
        'redis',
        'opensearches',
        'kafkas',
      ],
      verbs: [
        'get',
        'list',
        'watch',
      ],
    },
  ],
};

local exoObjectStorageClusterRole = kube.ClusterRole('appcat:cloudcollector:exoscale:objectstorage') + {
  rules: [
    {
      apiGroups: [ '*' ],
      resources: [ 'namespaces' ],
      verbs: [ 'get', 'list' ],
    },
    {
      apiGroups: [ 'exoscale.crossplane.io' ],
      resources: [
        'buckets',
      ],
      verbs: [
        'get',
        'list',
        'watch',
      ],
    },
  ],
};

local cloudscaleClusterRole = kube.ClusterRole('appcat:cloudcollector:cloudscale') + {
  rules: [
    {
      apiGroups: [ '' ],
      resources: [ 'namespaces' ],
      verbs: [ 'get', 'list' ],
    },
    {
      apiGroups: [ 'cloudscale.crossplane.io' ],
      resources: [
        'buckets',
      ],
      verbs: [
        'get',
        'list',
        'watch',
      ],
    },
  ],
};

local serviceAccount(name, clusterRole) = {
  local sa = kube.ServiceAccount(name) + {
    metadata+: {
      namespace: params.namespace,
    },
  },
  local rb = kube.ClusterRoleBinding(name) {
    roleRef_: clusterRole,
    subjects_: [ sa ],
  },
  sa: sa,
  rb: rb,
};

local deployment(name, args, cm) =
  kube.Deployment(name) {
    metadata+: {
      labels+: labels,
      namespace: params.namespace,
    },
    spec+: {
      template+: {
        spec+: {
          serviceAccount: name,
          containers_:: {
            exporter: kube.Container('exporter') {
              imagePullPolicy: 'IfNotPresent',
              image: collectorImage,
              args: args,
              envFrom: [
                {
                  configMapRef: {
                    name: cm,
                  },
                },
                {
                  secretRef: {
                    name: 'credentials-' + name,
                  },
                },
              ],
              ports_:: {
                exporter: {
                  containerPort: 9123,
                },
              },
              readinessProbe: healthProbe,
              livenessProbe: healthProbe,
            },
          },
        },
      },
    },
  };

local config(name, extraConfig) = kube.ConfigMap(name) {
  metadata: {
    name: name,
    namespace: params.namespace,
  },
  data: {
    ODOO_URL: std.toString(params.odoo.url),
    ODOO_OAUTH_TOKEN_URL: std.toString(params.odoo.tokenUrl),
    CLUSTER_ID: std.toString(params.clusterId),
    APPUIO_MANAGED_SALES_ORDER: if params.appuioManaged.enabled then std.toString(params.appuioManaged.salesOrder) else '',
    TENANT_ID: if params.appuioManaged.enabled then std.toString(params.appuioManaged.tenant) else '',
    PROM_URL: if params.appuioCloud.enabled then std.toString(params.appuioCloud.promUrl) else '',
  },
} + extraConfig;

({
    local odoo = params.secrets.odoo,
    assert odoo.credentials != null : 'odoo.credentials must be set.',
    assert odoo.credentials.stringData != null : 'odoo.credentials.stringData must be set.',
    assert odoo.credentials.stringData.ODOO_OAUTH_CLIENT_ID != null : 'odoo.credentials.stringData.ODOO_OAUTH_CLIENT_ID must be set.',
    assert odoo.credentials.stringData.ODOO_OAUTH_CLIENT_SECRET != null : 'odoo.credentials.stringData.ODOO_OAUTH_CLIENT_SECRET must be set.',
})
+
(if params.exoscale.enabled && params.exoscale.dbaas.enabled then {
   local name = 'exoscale-dbaas',
   local secrets = params.secrets.exoscale,
   local sa = serviceAccount(name, exoDbaasClusterRole),
   local extraConfig = {
       data+: {
         COLLECT_INTERVAL: std.toString(params.exoscale.dbaas.collectInterval),
       }
   },
   local cm = config(name + '-env', extraConfig),

   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_KEY != null : 'secrets.credentials.stringData.EXOSCALE_API_KEY must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_SECRET != null : 'secrets.credentials.stringData.EXOSCALE_API_SECRET must be set.',

   exoDbaasSecrets: std.filter(function(it) it != null, secret('exoscale', 'dbaas')),
   exoDbaasClusterRole: exoDbaasClusterRole,
   exoDbaasServiceAccount: sa.sa,
   exoDbaasRoleBinding: sa.rb,
   exoDbaasConfigMap: cm,
   exoDbaasExporter: deployment(name, [ 'exoscale', 'dbaas' ], name + '-env'),

 } else {})
+
(if params.exoscale.enabled && params.exoscale.objectStorage.enabled then {
   local name = 'exoscale-objectstorage',
   local secrets = params.secrets.exoscale,
   local sa = serviceAccount(name, exoObjectStorageClusterRole),
   local extraConfig = {
       data+: {
         COLLECT_INTERVAL: std.toString(params.exoscale.objectStorage.collectInterval),
         BILLING_HOUR: std.toString(params.exoscale.objectStorage.billingHour),
       }
   },
   local cm = config(name + '-env', extraConfig),

   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_KEY != null : 'secrets.credentials.stringData.EXOSCALE_API_KEY must be set.',
   assert secrets.credentials.stringData.EXOSCALE_API_SECRET != null : 'secrets.credentials.stringData.EXOSCALE_API_SECRET must be set.',

   exoObjectStorageSecrets: std.filter(function(it) it != null, secret('exoscale', 'objectstorage')),
   exoObjectStorageClusterRole: exoObjectStorageClusterRole,
   exoObjectStorageServiceAccount: sa.sa,
   exoObjectStorageRoleBinding: sa.rb,
   exoObjectStorageConfigMap: cm,
   exoObjectStorageExporter: deployment(name, [ 'exoscale', 'objectstorage' ], name + '-env'),

 } else {})
 +
(if params.cloudscale.enabled then {
   local name = 'cloudscale',
   local secrets = params.secrets.cloudscale,
   local sa = serviceAccount(name, cloudscaleClusterRole),
   local extraConfig = {
       data+: {
         COLLECT_INTERVAL: std.toString(params.cloudscale.collectInterval),
         BILLING_HOUR: std.toString(params.cloudscale.billingHour),
       }
   },
   local cm = config(name + '-env', extraConfig),

   assert secrets != null : 'secrets must be set.',
   assert secrets.credentials != null : 'secrets.credentials must be set.',
   assert secrets.credentials.stringData != null : 'secrets.credentials.stringData must be set.',
   assert secrets.credentials.stringData.CLOUDSCALE_API_TOKEN != null : 'secrets.credentials.stringData.CLOUDSCALE_API_TOKEN must be set.',

   cloudscaleSecrets: std.filter(function(it) it != null, secret(name, '')),
   cloudscaleClusterRole: cloudscaleClusterRole,
   cloudscaleServiceAccount: sa.sa,
   cloudscaleRolebinding: sa.rb,
   cloudscaleConfigMap: cm,
   cloudscaleExporter: deployment(name, [ 'cloudscale', 'objectstorage' ], name + '-env'),
 } else {})
