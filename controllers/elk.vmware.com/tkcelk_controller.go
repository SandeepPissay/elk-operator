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

package elkvmwarecom

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"k8s.io/apimachinery/pkg/api/errors"
	"os"
	"os/exec"
	"time"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elkvmwarecomv1alpha1 "github.com/SandeepPissay/elk-operator/apis/elk.vmware.com/v1alpha1"
)

// TkcElkReconciler reconciles a TkcElk object
type TkcElkReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	TkcSetup          = "TKC_SETUP"
	FileBeatInstall   = "FILEBEAT_INSTALL"
	MetricBeatInstall = "METRICBEAT_INSTALL"

	StepStatusPass = "PASS"
	StepStatusFail = "FAIL"
)

//+kubebuilder:rbac:groups=elk.vmware.com.vmware.com,resources=tkcelks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=elk.vmware.com.vmware.com,resources=tkcelks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=elk.vmware.com.vmware.com,resources=tkcelks/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the TkcElk object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.7.2/pkg/reconcile
func (r *TkcElkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("tkcelk", req.NamespacedName)

	tkcElkInstance := &elkvmwarecomv1alpha1.TkcElk{}
	err := r.Get(ctx, req.NamespacedName, tkcElkInstance)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("TkcElk CR is deleted. Ignoring workflows since it is deleted.")
			return ctrl.Result{}, nil
		}
		r.Log.Error(err, "Failed to get tkcElkInstance")
		return ctrl.Result{}, err
	}
	r.Log.Info(fmt.Sprintf("Loaded tkcElkInstance: %+v", tkcElkInstance))

	// Step 1: Setup elastic beats in TKC.
	err = r.setupTkc(ctx, tkcElkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to setup TKC. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 2: Install file beats in TKC.
	err = r.installFileBeat(ctx, tkcElkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install filebeat. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}

	// Step 3: Install metric beats in TKC.
	err = r.installMetricBeat(ctx, tkcElkInstance)
	if err != nil {
		r.Log.Error(err, "Failed to install metricbeat. Requeuing after 30 seconds.")
		return ctrl.Result{RequeueAfter: 30 * time.Second}, err
	}
	return ctrl.Result{}, nil
}

func (r *TkcElkReconciler) installFileBeat(ctx context.Context, tkcElkInstance *elkvmwarecomv1alpha1.TkcElk) error {
	stepStatus := r.getStepStatus(tkcElkInstance, FileBeatInstall)
	if stepStatus == nil {
		stepStatus = &elkvmwarecomv1alpha1.StepStatus{Step: FileBeatInstall}
		tkcElkInstance.Status.StepStatusDetails = append(tkcElkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/tkgs-elk/filebeat/examples/security", "sh", "./cleanup.sh",
			tkcElkInstance.Namespace, tkcElkInstance.Name, "observability")
		if err != nil {
			errMsg := "failed to cleanup filebeat."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/tkgs-elk/filebeat/examples/security", "sh", "./install.sh",
			tkcElkInstance.Namespace, tkcElkInstance.Name, "observability", tkcElkInstance.Spec.EsIpAddress)
		if err != nil {
			errMsg := "failed to install filebeat TKC."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark FILEBEAT_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *TkcElkReconciler) installMetricBeat(ctx context.Context, tkcElkInstance *elkvmwarecomv1alpha1.TkcElk) error {
	stepStatus := r.getStepStatus(tkcElkInstance, MetricBeatInstall)
	if stepStatus == nil {
		stepStatus = &elkvmwarecomv1alpha1.StepStatus{Step: MetricBeatInstall}
		tkcElkInstance.Status.StepStatusDetails = append(tkcElkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/tkgs-elk/metricbeat/examples/security", "sh", "./cleanup.sh",
			tkcElkInstance.Namespace, tkcElkInstance.Name, "observability")
		if err != nil {
			errMsg := "failed to cleanup metricbeat."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/tkgs-elk/metricbeat/examples/security", "sh", "./install.sh",
			tkcElkInstance.Namespace, tkcElkInstance.Name, "observability", tkcElkInstance.Spec.EsIpAddress)
		if err != nil {
			errMsg := "failed to install metricbeat TKC."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark METRICBEAT_INSTALL as success.")
			return err
		}
	}
	return nil
}

func (r *TkcElkReconciler) setupTkc(ctx context.Context, tkcElkInstance *elkvmwarecomv1alpha1.TkcElk) error {
	stepStatus := r.getStepStatus(tkcElkInstance, TkcSetup)
	if stepStatus == nil {
		stepStatus = &elkvmwarecomv1alpha1.StepStatus{Step: TkcSetup}
		tkcElkInstance.Status.StepStatusDetails = append(tkcElkInstance.Status.StepStatusDetails, *stepStatus)
	}
	if stepStatus.Status != StepStatusPass {
		err := r.runCommand("/tkgs-elk/tkgs-setup", "sh", "./cleanup.sh", tkcElkInstance.Namespace, tkcElkInstance.Name, "observability")
		if err != nil {
			errMsg := "failed to cleanup the TKC setup."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/tkgs-elk/tkgs-setup", "sh", "./setup.sh", tkcElkInstance.Namespace, tkcElkInstance.Name, "observability")
		if err != nil {
			errMsg := "failed to setup TKC."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
		stepStatus.ErrorMsg = ""
		err = r.updateTkcElkInstance(ctx, tkcElkInstance, stepStatus)
		if err != nil {
			r.Log.Error(err, "Failed to mark TKC_SETUP as success.")
			return err
		}
	}
	return nil
}

func (r *TkcElkReconciler) getStepStatus(elk *elkvmwarecomv1alpha1.TkcElk, step string) *elkvmwarecomv1alpha1.StepStatus {
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

func (r *TkcElkReconciler) runCommand(dir string, command string, args ...string) error {
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

func (r *TkcElkReconciler) updateTkcElkInstance(ctx context.Context, elk *elkvmwarecomv1alpha1.TkcElk, stepStatus *elkvmwarecomv1alpha1.StepStatus) error {
	for i, ssd := range elk.Status.StepStatusDetails {
		if ssd.Step == stepStatus.Step {
			elk.Status.StepStatusDetails[i] = *stepStatus
		}
	}
	r.Log.Info(fmt.Sprintf("Updating TkcElk: %+v. StepStatus: %+v", elk, stepStatus))
	err := r.Status().Update(ctx, elk)
	if err != nil {
		r.Log.Error(err, fmt.Sprintf("Failed to update elk: %+v", elk))
	}
	return err
}

// SetupWithManager sets up the controller with the Manager.
func (r *TkcElkReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&elkvmwarecomv1alpha1.TkcElk{}).
		Complete(r)
}
