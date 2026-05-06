/*
Copyright 2026.

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

package controller

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	networkingv1alpha1 "github.com/openshift/cudn-bgp-routing-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups=networking.openshift.io,resources=cudnbgpconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=networking.openshift.io,resources=cudnbgpconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=networking.openshift.io,resources=cudnbgpconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=networking.openshift.io,resources=cudnbgproutings,verbs=get;list;watch
// +kubebuilder:rbac:groups=operator.openshift.io,resources=networks,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=frrk8s.metallb.io,resources=frrconfigurations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch

type CUDNBgpConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *CUDNBgpConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	config := &networkingv1alpha1.CUDNBgpConfig{}
	if err := r.Get(ctx, req.NamespacedName, config); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	if !config.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, config)
	}

	if !controllerutil.ContainsFinalizer(config, ConfigFinalizerName) {
		controllerutil.AddFinalizer(config, ConfigFinalizerName)
		if err := r.Update(ctx, config); err != nil {
			return ctrl.Result{}, err
		}
	}

	config.Status.Phase = networkingv1alpha1.PhaseConfiguring
	config.Status.ObservedGeneration = config.Generation

	// Phase 1: Patch Network Operator
	log.Info("Phase 1: patching Network operator")
	if err := PatchNetworkOperator(ctx, r.Client); err != nil {
		return r.setDegraded(ctx, config, networkingv1alpha1.ConditionNetworkOperatorPatched,
			"PatchFailed", fmt.Sprintf("failed to patch Network operator: %v", err))
	}
	meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type:               networkingv1alpha1.ConditionNetworkOperatorPatched,
		Status:             metav1.ConditionTrue,
		Reason:             "Patched",
		Message:            "Network operator patched with FRR and routeAdvertisements",
		ObservedGeneration: config.Generation,
	})

	// Phase 2: Wait for FRR
	log.Info("Phase 2: checking FRR readiness")
	ready, err := IsFRRReady(ctx, r.Client)
	if err != nil {
		return r.setDegraded(ctx, config, networkingv1alpha1.ConditionFRRNamespaceReady,
			"CheckFailed", fmt.Sprintf("failed to check FRR readiness: %v", err))
	}
	if !ready {
		meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
			Type:               networkingv1alpha1.ConditionFRRNamespaceReady,
			Status:             metav1.ConditionFalse,
			Reason:             "WaitingForFRR",
			Message:            "Waiting for openshift-frr-k8s namespace and pods",
			ObservedGeneration: config.Generation,
		})
		if err := r.Status().Update(ctx, config); err != nil {
			return ctrl.Result{}, err
		}
		log.Info("FRR not ready, requeueing")
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
	meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type:               networkingv1alpha1.ConditionFRRNamespaceReady,
		Status:             metav1.ConditionTrue,
		Reason:             "Ready",
		Message:            "FRR namespace and pods are running",
		ObservedGeneration: config.Generation,
	})

	// Phase 3: Apply FRR Configuration per AZ
	log.Info("Phase 3: applying FRR configurations")
	if err := EnsureFRRConfigurations(ctx, r.Client, config); err != nil {
		return r.setDegraded(ctx, config, networkingv1alpha1.ConditionFRRConfigurationApplied,
			"ApplyFailed", fmt.Sprintf("failed to apply FRR configurations: %v", err))
	}
	meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type:               networkingv1alpha1.ConditionFRRConfigurationApplied,
		Status:             metav1.ConditionTrue,
		Reason:             "Applied",
		Message:            fmt.Sprintf("Applied %d FRRConfiguration(s)", len(config.Spec.BGP.AvailabilityZones)),
		ObservedGeneration: config.Generation,
	})

	config.Status.Phase = networkingv1alpha1.PhaseReady
	if err := r.Status().Update(ctx, config); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("reconciliation complete", "phase", config.Status.Phase)
	return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil
}

func (r *CUDNBgpConfigReconciler) reconcileDelete(ctx context.Context, config *networkingv1alpha1.CUDNBgpConfig) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	routingList := &networkingv1alpha1.CUDNBgpRoutingList{}
	if err := r.List(ctx, routingList); err != nil {
		return ctrl.Result{}, err
	}
	if len(routingList.Items) > 0 {
		log.Info("deletion blocked: CUDNBgpRouting CRs still exist", "count", len(routingList.Items))
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	log.Info("cleaning up FRR configurations")
	if err := DeleteFRRConfigurations(ctx, r.Client); err != nil {
		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(config, ConfigFinalizerName)
	if err := r.Update(ctx, config); err != nil {
		return ctrl.Result{}, err
	}

	log.Info("finalizer removed, deletion complete")
	return ctrl.Result{}, nil
}

func (r *CUDNBgpConfigReconciler) setDegraded(
	ctx context.Context,
	config *networkingv1alpha1.CUDNBgpConfig,
	condType, reason, message string,
) (ctrl.Result, error) {
	config.Status.Phase = networkingv1alpha1.PhaseDegraded
	meta.SetStatusCondition(&config.Status.Conditions, metav1.Condition{
		Type:               condType,
		Status:             metav1.ConditionFalse,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: config.Generation,
	})
	if err := r.Status().Update(ctx, config); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: 30 * time.Second}, fmt.Errorf("%s: %s", reason, message)
}

func (r *CUDNBgpConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkingv1alpha1.CUDNBgpConfig{}).
		Named("cudnbgpconfig").
		Complete(r)
}
