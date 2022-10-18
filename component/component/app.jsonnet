local kap = import 'lib/kapitan.libjsonnet';
local inv = kap.inventory();
local paramsACR = inv.parameters.appuio_cloud_reporting;
local argocd = import 'lib/argocd.libjsonnet';

local instance = inv.parameters._instance;
local app = argocd.App(instance, paramsACR.namespace);

{
  [instance]: app,
}
