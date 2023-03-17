/*
Copyright 2021 SPIRE Authors.

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
	"context"
	"time"

	"github.com/go-logr/logr"
	"github.com/spiffe/spire-controller-manager/pkg/reconciler"
	"github.com/spiffe/spire-controller-manager/pkg/stringset"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PodReconciler reconciles a Pod object
type PodReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	Triggerer        reconciler.Triggerer
	IgnoreNamespaces stringset.StringSet
}

//+kubebuilder:rbac:groups=spire.spiffe.io,resources=clusterspiffeids,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=spire.spiffe.io,resources=clusterspiffeids/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=spire.spiffe.io,resources=clusterspiffeids/finalizers,verbs=update
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=endpoints,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *PodReconciler) Reconcile(ctx context.Context, req ctrl.Request) (_ ctrl.Result, err error) {
	if !r.IgnoreNamespaces.In(req.Namespace) {
		log.FromContext(ctx).V(1).Info("Triggering reconciliation")
		r.Triggerer.Trigger()
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *PodReconciler) SetupWithManager(mgr ctrl.Manager, log logr.Logger) error {
	// Index endpoints by UID. Later when we reconcile the Pod this will make it easy to find the associated endpoints
	// and auto populate DNS names.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Endpoints{}, reconciler.EndpointUID, func(rawObj client.Object) []string {
		endpoints := rawObj.(*corev1.Endpoints)
		var podUIDs []string
		for _, subset := range endpoints.Subsets {
			for _, address := range subset.Addresses {
				if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
					podUIDs = append(podUIDs, string(address.TargetRef.UID))
				}
			}
			for _, address := range subset.NotReadyAddresses {
				if address.TargetRef != nil && address.TargetRef.Kind == "Pod" {
					podUIDs = append(podUIDs, string(address.TargetRef.UID))
				}
			}
		}

		return podUIDs
	})
	if err != nil {
		log.Error(err, "unable to setup Endpoint Indexer, disabling autoPopulateDNSNames field of ClusterSPIFFEID CRD.", "reason", k8serrors.ReasonForError(err))
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Pod{}).
		Complete(r)
}
