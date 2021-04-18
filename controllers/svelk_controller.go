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
	"k8s.io/apimachinery/pkg/runtime"
	"os"
	"os/exec"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	elkv1alpha1 "github.com/SandeepPissay/elk-operator/api/v1alpha1"
)

// SvElkReconciler reconciles a SvElk object
type SvElkReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

const (
	EsCreateSecret = "ES_CREATE_SECRET"

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

	err = r.createEsSecrets(ctx, svelkInstance)
	if err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, err
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
			err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		err = r.runCommand("/wcp-elk/elasticsearch/examples/security", "sh", "./create_secrets.sh", "observability")
		if err != nil {
			errMsg := "failed to create elastic search secrets."
			r.Log.Error(err, errMsg)
			stepStatus.Status = StepStatusFail
			stepStatus.ErrorMsg = fmt.Sprintf(errMsg+": %+v", err)
			err = r.updateSvElkInstance(ctx, svelkInstance, stepStatus)
			return err
		}
		// Success case
		stepStatus.Status = StepStatusPass
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
