package jobs

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/aquasecurity/trivy-kubernetes/pkg/k8s"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	k8sapierror "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	NodeCollectorName = "node-collector"

	// job headers
	TrivyCollectorName = "trivy.collector.name"
	TrivyAutoCreated   = "trivy.automatic.created"
	TrivyResourceName  = "trivy.resource.name"
	TrivyResourceKind  = "trivy.resource.kind"
)

type Collector interface {
	ApplyAndCollect(ctx context.Context, nodeName string) (string, error)
	Apply(ctx context.Context, nodeName string) (*batchv1.Job, error)
	AppendLabels(opts ...CollectorOption)
	Cleanup(ctx context.Context)
}

type jobCollector struct {
	cluster k8s.Cluster
	// timeout duration for collection job to complete it task before is cancelled default 0
	timeout              time.Duration
	logsReader           LogsReader
	labels               map[string]string
	annotation           map[string]string
	templateName         string
	namespace            string
	priorityClassName    string
	name                 string
	serviceAccount       string
	podSecurityContext   *corev1.PodSecurityContext
	securityContext      *corev1.SecurityContext
	imageRef             string
	affinity             *corev1.Affinity
	tolerations          []corev1.Toleration
	volumes              []corev1.Volume
	volumeMounts         []corev1.VolumeMount
	imagePullSecrets     []corev1.LocalObjectReference
	collectorTimeout     time.Duration
	resourceRequirements *corev1.ResourceRequirements
	nodeConfig           bool
	useNodeSelector      bool
}

type CollectorOption func(*jobCollector)

func WithTimetout(timeout time.Duration) CollectorOption {
	return func(jc *jobCollector) {
		jc.timeout = timeout
	}
}

func WithJobLabels(labels map[string]string) CollectorOption {
	return func(jc *jobCollector) {
		if jc.labels == nil {
			jc.labels = make(map[string]string)
		}
		for name, value := range labels {
			jc.labels[name] = value
		}
	}
}

func WithJobAnnotation(annotation map[string]string) CollectorOption {
	return func(jc *jobCollector) {
		jc.annotation = annotation
	}
}

func WithJobNamespace(namespace string) CollectorOption {
	return func(jc *jobCollector) {
		jc.namespace = namespace
	}
}

func WithPodPriorityClassName(priorityClassName string) CollectorOption {
	return func(jc *jobCollector) {
		jc.priorityClassName = priorityClassName
	}
}

func WithJobAffinity(affinity *corev1.Affinity) CollectorOption {
	return func(jc *jobCollector) {
		jc.affinity = affinity
	}
}

func WithJobTolerations(tolerations []corev1.Toleration) CollectorOption {
	return func(jc *jobCollector) {
		jc.tolerations = tolerations
	}
}

func WithName(name string) CollectorOption {
	return func(jc *jobCollector) {
		jc.name = name
	}
}

func WithNodeConfig(nodeConfig bool) CollectorOption {
	return func(jc *jobCollector) {
		jc.nodeConfig = nodeConfig
	}
}

func WithImageRef(imageRef string) CollectorOption {
	return func(jc *jobCollector) {
		jc.imageRef = imageRef
	}
}

func WithServiceAccount(sa string) CollectorOption {
	return func(jc *jobCollector) {
		jc.serviceAccount = sa
	}
}

func WithJobTemplateName(name string) CollectorOption {
	return func(jc *jobCollector) {
		jc.templateName = name
	}
}

func WithContainerResourceRequirements(rr *corev1.ResourceRequirements) CollectorOption {
	return func(j *jobCollector) {
		j.resourceRequirements = rr
	}
}

func WithContainerSecurityContext(securityContext *corev1.SecurityContext) CollectorOption {
	return func(jc *jobCollector) {
		jc.securityContext = securityContext
	}
}

func WithPodSpecSecurityContext(podSecurityContext *corev1.PodSecurityContext) CollectorOption {
	return func(jc *jobCollector) {
		jc.podSecurityContext = podSecurityContext
	}
}

func WithVolumes(volumes []corev1.Volume) CollectorOption {
	return func(jc *jobCollector) {
		jc.volumes = volumes
	}
}
func WithVolumesMount(volumesMount []corev1.VolumeMount) CollectorOption {
	return func(jc *jobCollector) {
		jc.volumeMounts = volumesMount
	}
}

func WithPodImagePullSecrets(imagePullSecrets []corev1.LocalObjectReference) CollectorOption {
	return func(jc *jobCollector) {
		jc.imagePullSecrets = imagePullSecrets
	}
}

func WithCollectorTimeout(timeout time.Duration) CollectorOption {
	return func(jc *jobCollector) {
		jc.collectorTimeout = timeout
	}
}

func WithUseNodeSelector(useNodeSelector bool) CollectorOption {
	return func(jc *jobCollector) {
		jc.useNodeSelector = useNodeSelector
	}
}

func NewCollector(
	cluster k8s.Cluster,
	opts ...CollectorOption,
) Collector {
	jc := &jobCollector{
		cluster:    cluster,
		timeout:    0,
		logsReader: NewLogsReader(cluster.GetK8sClientSet()),
	}
	for _, opt := range opts {
		opt(jc)
	}
	return jc
}

// AppendLabels Append labels to job
func (jb *jobCollector) AppendLabels(opts ...CollectorOption) {
	for _, opt := range opts {
		opt(jb)
	}
}

type ObjectRef struct {
	Kind      string
	Name      string
	Namespace string
}

// ApplyAndCollect deploy k8s job by template to  specific node  and namespace, it read pod logs
// cleaning up job and returning it output (for cli use-case)
func (jb *jobCollector) ApplyAndCollect(ctx context.Context, nodeName string) (string, error) {

	_, err := jb.getTrivyNamespace(ctx)
	if err != nil {
		if k8sapierror.IsNotFound(err) {
			trivyNamespace := &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: jb.namespace}}
			_, err = jb.cluster.GetK8sClientSet().CoreV1().Namespaces().Create(ctx, trivyNamespace, metav1.CreateOptions{})
			if err != nil {
				return "", err
			}
		}
	}
	if jb.nodeConfig {
		cr, rb, sa, err := GetAuth(WithServiceAccountNamespace(jb.namespace))
		if err != nil {
			return "", fmt.Errorf("running node-collector job: %w", err)
		}
		_, err = jb.cluster.GetK8sClientSet().RbacV1().ClusterRoles().Create(ctx, cr, metav1.CreateOptions{})
		if err != nil {
			return "", fmt.Errorf("creating cluster role: %w", err)
		}
		_, err = jb.cluster.GetK8sClientSet().CoreV1().ServiceAccounts(jb.namespace).Create(ctx, sa, metav1.CreateOptions{})
		if err != nil {
			return "", fmt.Errorf("creating service account: %w", err)
		}
		_, err = jb.cluster.GetK8sClientSet().RbacV1().ClusterRoleBindings().Create(ctx, rb, metav1.CreateOptions{})
		if err != nil {
			return "", fmt.Errorf("creating role binding: %w", err)
		}
	}

	JobOptions := []JobOption{
		WithTemplate(jb.templateName),
		WithNamespace(jb.namespace),
		WithNodeName(nodeName),
		WithAnnotation(jb.annotation),
		WithLabels(jb.labels),
		WithJobTimeout(jb.collectorTimeout),
		withSecurityContext(jb.securityContext),
		withPodSecurityContext(jb.podSecurityContext),
		WithNodeCollectorImageRef(jb.imageRef),
		WithAffinity(jb.affinity),
		WithTolerations(jb.tolerations),
		WithPodVolumes(jb.volumes),
		WithImagePullSecrets(jb.imagePullSecrets),
		WithContainerVolumeMounts(jb.volumeMounts),
		WithNodeConfiguration(jb.nodeConfig),
		WithPriorityClassName(jb.priorityClassName),
		WithResourceRequirements(jb.resourceRequirements),
		WithUseNodeSelectorParam(true),
		WithJobName(fmt.Sprintf("%s-%s", jb.templateName, ComputeHash(
			ObjectRef{
				Kind:      "Node-Info",
				Name:      nodeName,
				Namespace: jb.namespace,
			}))),
	}
	if jb.nodeConfig {
		JobOptions = append(JobOptions, WithJobServiceAccount(serviceAccount))
	}
	job, err := GetJob(JobOptions...)
	if err != nil {
		return "", fmt.Errorf("running node-collector job: %w", err)
	}

	err = New(WithTimeout(jb.timeout)).Run(ctx, NewRunnableJob(jb.cluster.GetK8sClientSet(), job))
	if err != nil {
		return "", fmt.Errorf("running node-collector job: %w", err)
	}
	defer func() {
		background := metav1.DeletePropagationBackground
		if jb.nodeConfig {
			_ = jb.cluster.GetK8sClientSet().RbacV1().ClusterRoleBindings().Delete(ctx, roleBinding, metav1.DeleteOptions{
				PropagationPolicy: &background,
			})
			_ = jb.cluster.GetK8sClientSet().RbacV1().ClusterRoles().Delete(ctx, clusterRole, metav1.DeleteOptions{
				PropagationPolicy: &background,
			})
			_ = jb.cluster.GetK8sClientSet().CoreV1().ServiceAccounts(job.Namespace).Delete(ctx, serviceAccount, metav1.DeleteOptions{
				PropagationPolicy: &background,
			})
		}
		_ = jb.cluster.GetK8sClientSet().BatchV1().Jobs(job.Namespace).Delete(ctx, job.Name, metav1.DeleteOptions{
			PropagationPolicy: &background,
		})
	}()

	logsStream, err := jb.logsReader.GetLogsByJobAndContainerName(ctx, job, NodeCollectorName)
	if err != nil {
		return "", fmt.Errorf("getting logs: %w", err)
	}
	defer func() {
		_ = logsStream.Close()
	}()
	output, err := io.ReadAll(logsStream)
	if err != nil {
		return "", fmt.Errorf("reading logs: %w", err)
	}
	return string(output), nil
}

// Apply deploy k8s job by template to specific node and namespace (for operator use case)
func (jb *jobCollector) Apply(ctx context.Context, nodeName string) (*batchv1.Job, error) {
	jobOptions := []JobOption{
		WithNamespace(jb.namespace),
		WithLabels(jb.labels),
		withPodSecurityContext(jb.podSecurityContext),
		withSecurityContext(jb.securityContext),
		WithAffinity(jb.affinity),
		WithTolerations(jb.tolerations),
		WithJobServiceAccount(jb.serviceAccount),
		WithJobTimeout(jb.collectorTimeout),
		WithNodeCollectorImageRef(jb.imageRef),
		WithAnnotation(jb.annotation),
		WithTemplate(jb.templateName),
		WithPodVolumes(jb.volumes),
		WithNodeConfiguration(jb.nodeConfig),
		WithImagePullSecrets(jb.imagePullSecrets),
		WithContainerVolumeMounts(jb.volumeMounts),
		WithPriorityClassName(jb.priorityClassName),
		WithNodeName(nodeName),
		WithJobName(jb.name),
		WithUseNodeSelectorParam(jb.useNodeSelector),
		WithResourceRequirements(jb.resourceRequirements)}

	job, err := GetJob(jobOptions...)
	if err != nil {
		return nil, fmt.Errorf("running node-collector job: %w", err)
	}
	// create job
	job, err = jb.cluster.GetK8sClientSet().BatchV1().Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err != nil {
		return nil, err
	}
	return job, nil
}

func (jb *jobCollector) deleteTrivyNamespace(ctx context.Context) {
	background := metav1.DeletePropagationBackground
	_ = jb.cluster.GetK8sClientSet().CoreV1().Namespaces().Delete(ctx, jb.namespace, metav1.DeleteOptions{
		PropagationPolicy: &background,
	})
}

func (jb *jobCollector) getTrivyNamespace(ctx context.Context) (*corev1.Namespace, error) {
	return jb.cluster.GetK8sClientSet().CoreV1().Namespaces().Get(ctx, jb.namespace, metav1.GetOptions{})
}

func (jb *jobCollector) Cleanup(ctx context.Context) {
	jb.deleteTrivyNamespace(ctx)
}
