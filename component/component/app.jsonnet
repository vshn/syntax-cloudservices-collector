local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local params = inv.parameters.exoscale_metrics_collector;
local paramsACR = inv.parameters.appuio_cloud_reporting;
local argocd = import 'lib/argocd.libjsonnet';

local app = argocd.App('exoscale-metrics-collector', paramsACR.namespace);

{
  'exoscale-metrics-collector': app,
}
