# elk-operator
ELK Operator for Kubernetes on vSphere.

# elk-operator for vSphere with Tanzu
ELK Operator for vSphere with Tanzu does the following -
- Deploys a highly available and secure ELK stack in the supervisor cluster that includes Elastic Search, Kibana, Filebeat, Metricbeat and APM-server.
- Automatically detects running TKG service clusters in the supervisor cluster and deploys the Filebeat, Metricbeat and APM-server that pushes data from TKG service cluster to the central elastic search data service deployed in the supervisor cluster.

## Why ELK for vSphere with Tanzu
vSphere with Tanzu solution offers customers to deploy their traditional and modern workloads on vSphere. This stack contains the core vSphere product, the supervisor cluster and Tanzu services(TKGS clusters). While working on CSI and CNS components in this stack, we realized few gaps in the way we are building these components, testing it and also debugging issues. Currently devs rely on support bundles to root cause issues, stabilize the product during development, and also to support VMware customers. This is not efficient for many reasons:
- Kubernetes and CSI logs get rolled over very quickly. There are many examples where devs ask for a repro since the error state is no longer available. In fact, on many occasions we ask for support bundle/logs only to realize that we do not have the logs for the time when the issue is observed thereby delaying the root cause analysis.
- Many genuine issues like pod crash, API failures, etc are not root caused since Kubernetes is an eventual consistent system, so everything eventually succeeds, thereby the test teams do not report issues.
- Root causing issues take a long time(sometimes in weeks) to triage since it hops from one component to another until the root cause is determined.

The above issues are more apparent in a scaled environment where many concurrent operations are triggered for a long duration of time. The current way of root causing the problem by asking for support bundles from the entire stack is not scalable. Because of this the overall time to root cause issues and providing a code fix is highly inefficient. However, the above mentioned problems are well known problems and are solved using observability - centralized logging, monitoring, metrics and tracing.

## Deploying elk-operator in vSphere with Tanzu
### Prerequisites
1. Create “elk-operator-system” SV namespace.
2. Create “observability” SV namespace. Assign “vSAN Default Storage Policy” to “observability” SV namespace. Provide at least 90GB quota for this policy on “observability” namespace.
3. Copy the .kube/config from the SV master VM into your laptop under ~/.kube directory. Modify the server settings in this file to point to one of the SV master VM IP. `kubectl` commands in your laptop should run against the vSphere with Tanzu cluster now. Note that we are using the kubernetes-admin account.

Note: None of the steps will be required once the vSphere with Tanzu product and elk-operator is enhanced to perform the above steps automatically.

### Installation
1. Git clone the repo and cd into elk-operator

`$ git clone git@github.com:SandeepPissay/elk-operator.git && cd elk-operator`

2. Deploy elk-operator

`$ make deploy IMG="quay.io/sandeeppissay/elk-operator:v0.1.0"`

3. Wait for the operator to reach “running” status.
4. Create the SvElk CR to trigger the operator to deploy the ELK stack:

`$ kubectl create -f config/samples/elk_v1alpha1_svelk.yaml`

5. Wait for the elastic search, Kibana, metricbeat and apm-server pods in "observability" namespace to reach "running" status. Wait for the filebeat pods in "vmware-system-beats" namespace to reach "running" stattus. 

It takes around less than 10 mins to fully deploy the ELK stack on vSphere with Tanzu running a virtual infrastructure. The elk-operator then watches for any existing or new TKGS cluster and deploys ELK stack automatically in it.

# Contact
Contact Sandeep Pissay (ssrinivas@vmware.com) to get any information on the elk-operator or to get involved with development of the operator.

# Known bugs

# Troubleshooting
