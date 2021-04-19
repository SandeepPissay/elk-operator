/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"bytes"
	"context"
	"fmt"
	"github.com/go-logr/logr"
	"io"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"os"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"

	elkv1alpha1 "github.com/SandeepPissay/elk-operator/api/v1alpha1"
)

// SvElkReconciler reconciles a SvElk object
type SvElkReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	EsCreateSecret    = "ES_CREATE_SECRET"
	EsInstall         = "ES_INSTALL"
	KibanaInstall     = "KIBANA_INSTALL"
	FileBeatInstall   = "FILEBEAT_INSTALL"
	MetricBeatInstall = "METRICBEAT_INSTALL"
	ApmServerInstall  = "APMSERVER_INSTALL"

	StepStatusPass = "PASS"
	StepStatusFail = "FAIL"
)

//+kubebuilder:rbac:groups=elk.vmware.com,resources=svelks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=elk.vmware.com,resources=svelks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=elk.vmware.com,resources=svelks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SvElk object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *SvElkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("svelk", req.NamespacedName)
	r.Log.Info("reconciling SvElkReconciler", "instance", req)

	if req.Namespace != "elk-operator-system" || req.Name != "elk" {
		r.Log.Info("Ignoring other SvElk CRs...")
		return ctrl.Result{}, nil
	}

	svelkInstance := &elkv1alpha1.SvElk{}
	err := r.Get(ctx, req.NamespacedName, svelkInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("SvElk CR is deleted. Ignoring workflows since it is deleted.")
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get svelkInstance")
		return ctrl.Result{}, err
	}
	r.Log.Info(fmt.Sprintf("Loaded svelkInstance: %+v", svelkInstance))

	// Step 1: Create elastic search secrets.
	err = r.createEsSecrets(ctx, svelkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to create secrets for elastic search. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 2: Install elastic search.
	err = r.installElasticSearch(ctx, svelkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install elastic search. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 3: Install Kibana.
	err = r.installKibana(ctx, svelkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install Kibana. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 4: Install file beats for log forwarding.
	err = r.installFileBeat(ctx, svelkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install FileBeat. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 5: Install metric beats to collect metrics.
	err = r.installMetricBeat(ctx, svelkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install MetricBeat. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 6: Install apm-server to collect traces.
	err = r.installApmServer(ctx, svelkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install ApmServer. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	go r.pollTkcAndDeployElk()
	return ctrl.Result{}, err
}

func (r *SvElkReconciler) pollTkcAndDeployElk() {
	ticker := time.NewTicker(time.Duration(10) * time.Second)
	defer ticker.Stop()
	for {
		config, err := rest.InClusterConfig()
		if err != nil {
			r.Log.Error(err, "Cannot get in cluster config. Will retry in 10 seconds")
			time.Sleep(time.Duration(10) * time.Second)
			continue
		}

		dynamicClient, err := dynamic.NewForConfig(config)
		if err != nil {
			r.Log.Error(err, "Cannot get dynamic client from the in cluster config. Will retry in 10 seconds")
			time.Sleep(time.Duration(10) * time.Second)
			continue
		}

		tkcRes := schema.GroupVersionResource{
			Group:    "run.tanzu.vmware.com",
			Version:  "v1alpha1",
			Resource: "tanzukubernetesclusters",
		}

		select {
		case <-ticker.C:
			r.Log.Info("In pollTkcAndDeployElk")
			tkcList, err := dynamicClient.Resource(tkcRes).List(context.TODO(), metav1.ListOptions{})
			if err != nil {
				r.Log.Error(err, "Error while listing tkcs.")
				continue
			}
			for _, d := range tkcList.Items {
				r.Log.Info(fmt.Sprintf("Object: %+v", d.Object))
				phase, found, err := unstructured.NestedString(d.Object, "status", "phase")
				if err != nil {
					r.Log.Error(err, "Error while parsing status.phase.")
					continue
				}
				if found {
					if phase == "running" {
						r.Log.Info(fmt.Sprintf("TKC %s is found and running", d.GetName()))
					} else {
						r.Log.Info(fmt.Sprintf("TKC %s is found but not running. Phase: %s", d.GetName(), phase))
					}
				} else {
					r.Log.Info(fmt.Sprintf("TKC %s is not found", d.GetName()))
				}
			}
		}
	}
}

func (r *SvElkReconciler) installApmServer(ctx context.Context, svelkInstance *elkv1alpha1.SvElk) error {
	stepStatus := r.getStepStatus(svelkInstance, ApmServerInstall)
	if stepStatus == nil {
		stepStatus = &elkv1alpha1.StepStatus{Step: ApmServerInstall}
		svelkInstance.Status.StepStatusDetails = append(svelkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/wcp-elk/apm-server/examples/security", "sh", "./cleanup.sh", "observability")
		if err != nil {
			errMsg := "failed to cleanup apm-server."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/apm-server/examples/security", "sh", "./install.sh", "observability")
		if err != nil {
			errMsg := "failed to install apm-server."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark APMSERVER_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *SvElkReconciler) installMetricBeat(ctx context.Context, svelkInstance *elkv1alpha1.SvElk) error {
	stepStatus := r.getStepStatus(svelkInstance, MetricBeatInstall)
	if stepStatus == nil {
		stepStatus = &elkv1alpha1.StepStatus{Step: MetricBeatInstall}
		svelkInstance.Status.StepStatusDetails = append(svelkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/wcp-elk/metricbeat/examples/security", "sh", "./cleanup.sh", "observability")
		if err != nil {
			errMsg := "failed to cleanup metricbeat."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/metricbeat/examples/security", "sh", "./install.sh", "observability")
		if err != nil {
			errMsg := "failed to install metricbeat."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark METRICBEAT_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *SvElkReconciler) installFileBeat(ctx context.Context, svelkInstance *elkv1alpha1.SvElk) error {
	stepStatus := r.getStepStatus(svelkInstance, FileBeatInstall)
	if stepStatus == nil {
		stepStatus = &elkv1alpha1.StepStatus{Step: FileBeatInstall}
		svelkInstance.Status.StepStatusDetails = append(svelkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/wcp-elk/filebeat/examples/security", "sh", "./cleanup.sh", "vmware-system-beats")
		if err != nil {
			errMsg := "failed to cleanup file beat."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/filebeat/examples/security", "sh", "./install.sh", "vmware-system-beats")
		if err != nil {
			errMsg := "failed to install file beat."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark FILEBEAT_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *SvElkReconciler) installKibana(ctx context.Context, svelkInstance *elkv1alpha1.SvElk) error {
	stepStatus := r.getStepStatus(svelkInstance, KibanaInstall)
	if stepStatus == nil {
		stepStatus = &elkv1alpha1.StepStatus{Step: KibanaInstall}
		svelkInstance.Status.StepStatusDetails = append(svelkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/wcp-elk/kibana/examples/security", "sh", "./cleanup.sh", "observability")
		if err != nil {
			errMsg := "failed to cleanup kibana."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/kibana/examples/security", "sh", "./install.sh", "observability")
		if err != nil {
			errMsg := "failed to install kibana."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark KIBANA_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *SvElkReconciler) installElasticSearch(ctx context.Context, svelkInstance *elkv1alpha1.SvElk) error {
	stepStatus := r.getStepStatus(svelkInstance, EsInstall)
	if stepStatus == nil {
		stepStatus = &elkv1alpha1.StepStatus{Step: EsInstall}
		svelkInstance.Status.StepStatusDetails = append(svelkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/wcp-elk/elasticsearch/examples/security", "sh", "./cleanup.sh", "observability")
		if err != nil {
			errMsg := "failed to cleanup elastic search."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/elasticsearch/examples/security", "sh", "./install.sh", "observability")
		if err != nil {
			errMsg := "failed to install elastic search."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark ES_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *SvElkReconciler) createEsSecrets(ctx context.Context, svelkInstance *elkv1alpha1.SvElk) error {
	stepStatus := r.getStepStatus(svelkInstance, EsCreateSecret)
	if stepStatus == nil {
		stepStatus = &elkv1alpha1.StepStatus{Step: EsCreateSecret}
		svelkInstance.Status.StepStatusDetails = append(svelkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/wcp-elk/elasticsearch/examples/security", "sh", "./delete_secrets.sh", "observability")
		if err != nil {
			errMsg := "failed to delete elastic search secrets."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/elasticsearch/examples/security", "sh", "./create_secrets.sh", "observability")
		if err != nil {
			errMsg := "failed to create elastic search secrets."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark ES_CREATE_SECRET as success.")
			return err
		}
	}
	return nil
}

func (r *SvElkReconciler) updateSvElkInstance(ctx context.Context, elk *elkv1alpha1.SvElk, stepStatus *elkv1alpha1.StepStatus) error {
	for i, ssd := range elk.Status.StepStatusDetails {
		if ssd.Step == stepStatus.Step {
			elk.Status.StepStatusDetails[i] = *stepStatus
		}
	}
	r.Log.Info(fmt.Sprintf("Updating SvElk: %+v. StepStatus: %+v", elk, stepStatus))
	err := r.Status().Update(ctx, elk)
	if err != nil {
		r.Log.Error(err, fmt.Sprintf("Failed to update elk: %+v", elk))
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *SvElkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elkv1alpha1.SvElk{}).
		Complete(r)
}

func (r *SvElkReconciler) getStepStatus(elk *elkv1alpha1.SvElk, step string) *elkv1alpha1.StepStatus {
	if len(elk.Status.StepStatusDetails) == 0 {
		return nil
	}

	for _, ssd := range elk.Status.StepStatusDetails {
		if ssd.Step == step {
			return &ssd
		}
	}
	return nil
}

func (r *SvElkReconciler) runCommand(dir string, command string, args ...string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = dir

	var stdBuffer bytes.Buffer
	mw := io.MultiWriter(os.Stdout, &stdBuffer)
	cmd.Stdout = mw
	cmd.Stderr = mw
	// Execute the command
	err := cmd.Run()
	r.Log.Info(stdBuffer.String())
	return err
}
