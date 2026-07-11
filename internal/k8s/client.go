// Package k8s is CTRAIL's backend client for the Kubernetes API server, which
// plays the MEC orchestrator (MEAO/MEPM) role for the Edge Application
// Management instance. It wraps the official client-go typed clientset behind a
// small Client interface, exactly as internal/nef wraps net/http for the
// Traffic Influence instance. The interface keeps k8s.io/* imports out of the
// orchestrator/mapper and lets tests inject a fake clientset.
package k8s

import (
	"context"
	"fmt"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// Client is the subset of Kubernetes operations CTRAIL needs to orchestrate edge
// application deployments. All methods are namespace-scoped where relevant.
type Client interface {
	// EnsureNamespace creates the namespace if it does not already exist.
	EnsureNamespace(ctx context.Context, name string, labels map[string]string) error
	// ApplyDeployment creates the deployment, or replaces it if it already exists.
	ApplyDeployment(ctx context.Context, ns string, d *appsv1.Deployment) (*appsv1.Deployment, error)
	// GetDeployment returns the deployment, or an error for which IsNotFound reports true.
	GetDeployment(ctx context.Context, ns, name string) (*appsv1.Deployment, error)
	// PatchDeployment applies an RFC 7386 JSON merge patch to a deployment.
	PatchDeployment(ctx context.Context, ns, name string, mergePatch []byte) (*appsv1.Deployment, error)
	// ApplyService creates the service, or replaces it if it already exists.
	ApplyService(ctx context.Context, ns string, s *corev1.Service) (*corev1.Service, error)
	// DeleteDeployment removes a deployment; a not-found deletion is not an error.
	DeleteDeployment(ctx context.Context, ns, name string) error
	// DeleteService removes a service; a not-found deletion is not an error.
	DeleteService(ctx context.Context, ns, name string) error
	// DeleteNamespace removes a namespace; a not-found deletion is not an error.
	DeleteNamespace(ctx context.Context, name string) error
	// ListNamespaces returns namespaces matching the label selector ("" = all).
	ListNamespaces(ctx context.Context, labelSelector string) ([]corev1.Namespace, error)
}

// clientset is the concrete Client backed by a client-go kubernetes.Interface.
type clientset struct {
	cs kubernetes.Interface
}

// NewClient builds a Client from in-cluster config when kubeconfig is empty, or
// from the given kubeconfig file path otherwise.
func NewClient(kubeconfig string) (Client, error) {
	var cfg *rest.Config
	var err error

	if kubeconfig == "" {
		cfg, err = rest.InClusterConfig()
		if err != nil {
			return nil, fmt.Errorf("in-cluster kubernetes config: %w", err)
		}
	} else {
		cfg, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			return nil, fmt.Errorf("kubeconfig %q: %w", kubeconfig, err)
		}
	}

	cs, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("kubernetes clientset: %w", err)
	}
	return &clientset{cs: cs}, nil
}

// NewClientFromInterface wraps an existing kubernetes.Interface. Tests pass
// fake.NewSimpleClientset() here — the client-go analogue of the mock NEF.
func NewClientFromInterface(cs kubernetes.Interface) Client {
	return &clientset{cs: cs}
}

func (c *clientset) EnsureNamespace(ctx context.Context, name string, labels map[string]string) error {
	ns := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{Name: name, Labels: labels},
	}
	_, err := c.cs.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %q: %w", name, err)
	}
	return nil
}

func (c *clientset) ApplyDeployment(ctx context.Context, ns string, d *appsv1.Deployment) (*appsv1.Deployment, error) {
	deployments := c.cs.AppsV1().Deployments(ns)
	created, err := deployments.Create(ctx, d, metav1.CreateOptions{})
	if err == nil {
		return created, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create deployment %s/%s: %w", ns, d.Name, err)
	}

	existing, err := deployments.Get(ctx, d.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get deployment %s/%s for replace: %w", ns, d.Name, err)
	}
	d.ResourceVersion = existing.ResourceVersion
	updated, err := deployments.Update(ctx, d, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update deployment %s/%s: %w", ns, d.Name, err)
	}
	return updated, nil
}

func (c *clientset) GetDeployment(ctx context.Context, ns, name string) (*appsv1.Deployment, error) {
	return c.cs.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
}

func (c *clientset) PatchDeployment(ctx context.Context, ns, name string, mergePatch []byte) (*appsv1.Deployment, error) {
	return c.cs.AppsV1().Deployments(ns).Patch(ctx, name, types.MergePatchType, mergePatch, metav1.PatchOptions{})
}

func (c *clientset) ApplyService(ctx context.Context, ns string, s *corev1.Service) (*corev1.Service, error) {
	services := c.cs.CoreV1().Services(ns)
	created, err := services.Create(ctx, s, metav1.CreateOptions{})
	if err == nil {
		return created, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return nil, fmt.Errorf("create service %s/%s: %w", ns, s.Name, err)
	}

	existing, err := services.Get(ctx, s.Name, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("get service %s/%s for replace: %w", ns, s.Name, err)
	}
	s.ResourceVersion = existing.ResourceVersion
	s.Spec.ClusterIP = existing.Spec.ClusterIP // immutable; carry it over
	updated, err := services.Update(ctx, s, metav1.UpdateOptions{})
	if err != nil {
		return nil, fmt.Errorf("update service %s/%s: %w", ns, s.Name, err)
	}
	return updated, nil
}

func (c *clientset) DeleteDeployment(ctx context.Context, ns, name string) error {
	err := c.cs.AppsV1().Deployments(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete deployment %s/%s: %w", ns, name, err)
	}
	return nil
}

func (c *clientset) DeleteService(ctx context.Context, ns, name string) error {
	err := c.cs.CoreV1().Services(ns).Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete service %s/%s: %w", ns, name, err)
	}
	return nil
}

func (c *clientset) DeleteNamespace(ctx context.Context, name string) error {
	err := c.cs.CoreV1().Namespaces().Delete(ctx, name, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete namespace %q: %w", name, err)
	}
	return nil
}

func (c *clientset) ListNamespaces(ctx context.Context, labelSelector string) ([]corev1.Namespace, error) {
	list, err := c.cs.CoreV1().Namespaces().List(ctx, metav1.ListOptions{LabelSelector: labelSelector})
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}
	return list.Items, nil
}

// IsNotFound reports whether err is a Kubernetes "not found" API error. It lets
// callers map a missing deployment to a CAMARA 404 without importing k8s.io/*.
func IsNotFound(err error) bool {
	return apierrors.IsNotFound(err)
}
