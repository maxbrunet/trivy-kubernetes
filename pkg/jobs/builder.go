package jobs

import (
	"time"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/yaml"
)

type JobOption func(*JobBuilder)

func WithTemplate(template string) JobOption {
	return func(j *JobBuilder) {
		j.template = template
	}
}

func WithNodeName(nodeName string) JobOption {
	return func(j *JobBuilder) {
		j.nodeName = nodeName
	}
}
func WithJobName(name string) JobOption {
	return func(j *JobBuilder) {
		j.name = name
	}
}
func WithNamespace(namespace string) JobOption {
	return func(j *JobBuilder) {
		j.namespace = namespace
	}
}

func WithJobServiceAccount(sa string) JobOption {
	return func(j *JobBuilder) {
		j.serviceAccount = sa
	}
}

func WithLabels(labels map[string]string) JobOption {
	return func(j *JobBuilder) {
		j.labels = labels
	}
}

func WithAnnotation(annotations map[string]string) JobOption {
	return func(j *JobBuilder) {
		j.annotations = annotations
	}
}

func WithAffinity(affinity *corev1.Affinity) JobOption {
	return func(j *JobBuilder) {
		j.affinity = affinity
	}
}

func WithTolerations(tolerations []corev1.Toleration) JobOption {
	return func(j *JobBuilder) {
		j.tolerations = tolerations
	}
}

func WithPriorityClassName(priorityClassName string) JobOption {
	return func(j *JobBuilder) {
		j.priorityClassName = priorityClassName
	}
}

func WithNodeCollectorImageRef(imageRef string) JobOption {
	return func(j *JobBuilder) {
		j.imageRef = imageRef
	}
}

func withSecurityContext(securityContext *corev1.SecurityContext) JobOption {
	return func(j *JobBuilder) {
		j.securityContext = securityContext
	}
}

func withPodSecurityContext(podSecurityContext *corev1.PodSecurityContext) JobOption {
	return func(j *JobBuilder) {
		j.podSecurityContext = podSecurityContext
	}
}

func WithPodVolumes(volumes []corev1.Volume) JobOption {
	return func(j *JobBuilder) {
		j.volumes = volumes
	}
}
func WithContainerVolumeMounts(volumeMounts []corev1.VolumeMount) JobOption {
	return func(j *JobBuilder) {
		j.volumeMounts = volumeMounts
	}
}

func WithImagePullSecrets(imagePullSecrets []corev1.LocalObjectReference) JobOption {
	return func(j *JobBuilder) {
		j.imagePullSecrets = imagePullSecrets
	}
}

func WithResourceRequirements(rr *corev1.ResourceRequirements) JobOption {
	return func(j *JobBuilder) {
		j.resourceRequirements = rr
	}
}
func WithJobTimeout(timeout time.Duration) JobOption {
	return func(j *JobBuilder) {
		j.timeout = timeout
	}
}

func WithNodeConfiguration(nodeConfig bool) JobOption {
	return func(j *JobBuilder) {
		j.nodeConfig = nodeConfig
	}
}

func WithUseNodeSelectorParam(useNodeSelector bool) JobOption {
	return func(j *JobBuilder) {
		j.useNodeSelector = useNodeSelector
	}
}

func GetJob(opts ...JobOption) (*batchv1.Job, error) {
	jb := &JobBuilder{}
	for _, opt := range opts {
		opt(jb)
	}
	return jb.build()
}

type JobBuilder struct {
	template             string
	nodeName             string
	namespace            string
	imageRef             string
	serviceAccount       string
	name                 string
	labels               map[string]string
	podSecurityContext   *corev1.PodSecurityContext
	securityContext      *corev1.SecurityContext
	annotations          map[string]string
	affinity             *corev1.Affinity
	tolerations          []corev1.Toleration
	priorityClassName    string
	volumes              []corev1.Volume
	volumeMounts         []corev1.VolumeMount
	imagePullSecrets     []corev1.LocalObjectReference
	resourceRequirements *corev1.ResourceRequirements
	timeout              time.Duration
	nodeConfig           bool
	useNodeSelector      bool
}

func (b *JobBuilder) build() (*batchv1.Job, error) {
	template := getTemplate(b.template)
	var job batchv1.Job

	err := yaml.Unmarshal([]byte(template), &job)
	if err != nil {
		return nil, err
	}
	job.Namespace = b.namespace
	if len(b.name) > 0 {
		job.Name = b.name
	}
	if len(b.imageRef) > 0 {
		job.Spec.Template.Spec.Containers[0].Image = b.imageRef
	}
	if b.nodeConfig {
		job.Spec.Template.Spec.Containers[0].Args = append(job.Spec.Template.Spec.Containers[0].Args, "--node", b.nodeName)
	}
	if b.useNodeSelector {
		job.Spec.Template.Spec.NodeSelector = map[string]string{
			corev1.LabelHostname: b.nodeName,
		}
	}
	// append lables
	for key, val := range b.labels {
		if job.Labels == nil {
			job.Labels = make(map[string]string)
		}
		job.Labels[key] = val
	}
	// append annotation
	for key, val := range b.annotations {
		if job.Annotations == nil {
			job.Annotations = make(map[string]string)
		}
		job.Annotations[key] = val
	}
	if len(b.serviceAccount) > 0 {
		job.Spec.Template.Spec.ServiceAccountName = b.serviceAccount
	}
	if b.affinity != nil {
		job.Spec.Template.Spec.Affinity = b.affinity
	}
	if len(b.tolerations) > 0 {
		job.Spec.Template.Spec.Tolerations = b.tolerations
	}
	if b.priorityClassName != "" {
		job.Spec.Template.Spec.PriorityClassName = b.priorityClassName
	}
	if b.podSecurityContext != nil {
		job.Spec.Template.Spec.SecurityContext = b.podSecurityContext
	}
	if b.timeout > 0 {
		job.Spec.ActiveDeadlineSeconds = ptr.To[int64](int64(b.timeout.Seconds()))
	}
	if b.securityContext != nil {
		job.Spec.Template.Spec.Containers[0].SecurityContext = b.securityContext
	}
	if len(b.volumes) > 0 {
		job.Spec.Template.Spec.Volumes = b.volumes
	}
	if len(b.imagePullSecrets) > 0 {
		job.Spec.Template.Spec.ImagePullSecrets = b.imagePullSecrets
	}
	if b.resourceRequirements != nil {
		for _, c := range job.Spec.Template.Spec.Containers {
			c.Resources = *b.resourceRequirements
		}
	}
	if len(b.volumeMounts) > 0 {
		job.Spec.Template.Spec.Containers[0].VolumeMounts = b.volumeMounts
	}
	return &job, nil
}
