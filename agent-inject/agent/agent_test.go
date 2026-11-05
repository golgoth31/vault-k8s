// Copyright IBM Corp. 2019, 2025
// SPDX-License-Identifier: MPL-2.0

package agent

import (
	"encoding/json"
	"testing"

	jsonpatch "github.com/evanphx/json-patch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func testPod(annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "foo",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "foobar",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "foobar",
							MountPath: "serviceaccount/somewhere",
						},
						{
							Name:      "tobecopied",
							MountPath: "/etc/somewhereelse",
							ReadOnly:  false,
						},
					},
				},
				{
					Name: "foo1",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "foo1",
							MountPath: "/data/foo1",
						},
					},
				},
				{
					Name: "foo2",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "foo2",
							MountPath: "/data/foo2",
						},
					},
				},
			},
		},
	}
}

func testPodIRSA(annotations map[string]string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "foo",
			Annotations: annotations,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name: "foobar",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "foobar",
							MountPath: "serviceaccount/somewhere",
						},
					},
				},
				{
					Name: "aws-iam-token",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "aws-iam-token",
							MountPath: "/var/run/secrets/eks.amazonaws.com/serviceaccount",
						},
					},
				},
			},
		},
	}
}

func TestShouldInject(t *testing.T) {
	tests := []struct {
		annotations map[string]string
		phase       corev1.PodPhase
		inject      bool
	}{
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: ""}, corev1.PodPending, true},
		{map[string]string{AnnotationAgentInject: "false", AnnotationAgentStatus: ""}, corev1.PodPending, false},
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: "injected"}, corev1.PodPending, false},
		{map[string]string{AnnotationAgentInject: "false", AnnotationAgentStatus: "injected"}, corev1.PodPending, false},
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: "update"}, corev1.PodPending, true},
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: ""}, corev1.PodRunning, false},
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: ""}, corev1.PodSucceeded, false},
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: ""}, corev1.PodFailed, false},
		{map[string]string{AnnotationAgentInject: "true", AnnotationAgentStatus: "update"}, corev1.PodRunning, false},
	}

	for _, tt := range tests {
		pod := testPod(tt.annotations)
		pod.Status.Phase = tt.phase
		inject, err := ShouldInject(pod)
		if err != nil {
			t.Errorf("got error, shouldn't have: %s", err)
		}

		if inject != tt.inject {
			t.Errorf("expected should inject to be %v, got %v", tt.inject, inject)
		}
	}
}

func TestValidate(t *testing.T) {
	tests := []struct {
		agent Agent
		valid bool
	}{
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName:     "test",
				ConfigMapName: "test",
			}, true,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName: "test",
				Vault: Vault{
					Role:     "test",
					Address:  "https://foobar.com:8200",
					AuthPath: "test",
					AuthType: "kubernetes",
				},
			}, true,
		},
		{
			Agent{
				Namespace: "",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName:     "test",
				ConfigMapName: "test",
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName:     "test",
				ConfigMapName: "test",
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "",
					TokenPath: "foobar",
				},
				ImageName:     "test",
				ConfigMapName: "test",
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "",
				},
				ImageName:     "test",
				ConfigMapName: "test",
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName:     "",
				ConfigMapName: "test",
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName: "test",
				Vault: Vault{
					Role:     "",
					Address:  "https://foobar.com:8200",
					AuthType: "kubernetes",
				},
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName: "test",
				Vault: Vault{
					Role:     "test",
					Address:  "",
					AuthType: "kubernetes",
				},
			}, false,
		},
		{
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "foobar",
				},
				ImageName: "test",
				Vault: Vault{
					Role:     "test",
					Address:  "https://foobar.com:8200",
					AuthPath: "",
					AuthType: "kubernetes",
				},
			}, false,
		},
		{
			// Missing ServiceAccountTokenVolume.TokenPath
			Agent{
				Namespace: "test",
				ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
					Name:      "foobar",
					MountPath: "foobar",
					TokenPath: "",
				},
				ImageName: "test",
				Vault: Vault{
					Role:     "test",
					Address:  "https://foobar.com:8200",
					AuthPath: "test",
					AuthType: "",
				},
			}, false,
		},
		{
			// Missing ServiceAccountTokenVolume
			Agent{
				Namespace: "test",
				ImageName: "test",
				Vault: Vault{
					Role:     "test",
					Address:  "https://foobar.com:8200",
					AuthPath: "test",
					AuthType: "",
				},
			}, false,
		},
	}

	for _, tt := range tests {
		err := tt.agent.Validate()

		if err != nil && tt.valid {
			t.Errorf("got error, shouldn't have: %s", err)
		}

		if err == nil && !tt.valid {
			t.Error("got no error, should have")
		}
	}
}

func Test_serviceaccount(t *testing.T) {
	tests := map[string]struct {
		pod           *corev1.Pod
		expected      *ServiceAccountTokenVolume
		expectedError string
	}{
		"no service accounts": {
			expected:      nil,
			expectedError: "failed to find service account volume mount",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{},
			},
		},
		"missing token volume": {
			expected:      nil,
			expectedError: `failed to find service account volume "projected-token"`,
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						"vault.hashicorp.com/agent-service-account-token-volume-name": "projected-token",
					},
				},
			},
		},
		"token volume name mount missing from volumes": {
			expected:      nil,
			expectedError: `failed to find volume "missing" in Pod "test-pod" volumes`,
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Name: "test-pod",
					Annotations: map[string]string{
						"vault.hashicorp.com/agent-service-account-token-volume-name": "missing",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "missing",
									MountPath: "/not/a/projected/token/volume",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "projected-token",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
												Path: "token",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"missing tokenPath for volume that's mounted": {
			expected:      nil,
			expectedError: `failed to find tokenPath for projected volume "projected-token"`,
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						"vault.hashicorp.com/agent-service-account-token-volume-name": "projected-token",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "projected-token",
									MountPath: "/var/run/secrets/special/serviceaccount",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "projected-token",
						},
					},
				},
			},
		},
		"missing tokenPath in volume": {
			expected:      nil,
			expectedError: `failed to find tokenPath for projected volume "projected-token"`,
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						"vault.hashicorp.com/agent-service-account-token-volume-name": "projected-token",
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "projected-token",
						},
					},
				},
			},
		},
		"regular service account (not projected)": {
			expected: &ServiceAccountTokenVolume{
				Name:      "internal-app-token-n4pjn",
				MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
				TokenPath: "token",
			},
			expectedError: "",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "internal-app-token-n4pjn",
									MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "internal-app-token-n4pjn",
							VolumeSource: corev1.VolumeSource{
								Secret: &corev1.SecretVolumeSource{
									SecretName: "internal-app-token-n4pjn",
								},
							},
						},
					},
				},
			},
		},
		"projected default service account": {
			expected: &ServiceAccountTokenVolume{
				Name:      "kube-api-access-4bfzq",
				MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
				TokenPath: "token",
			},
			expectedError: "",
			pod: &corev1.Pod{
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "kube-api-access-4bfzq",
									MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "kube-api-access-4bfzq",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
												Path: "token",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"projected service account with annotation": {
			expected: &ServiceAccountTokenVolume{
				Name:      "projected-token",
				MountPath: "/var/run/secrets/special/serviceaccount",
				TokenPath: "vault-token",
			},
			expectedError: "",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						"vault.hashicorp.com/agent-service-account-token-volume-name": "projected-token",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "projected-token",
									MountPath: "/var/run/secrets/special/serviceaccount",
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "projected-token",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
												Path: "vault-token",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
		"projected service account with annotation but not mounted": {
			expected: &ServiceAccountTokenVolume{
				Name:      "projected-token",
				MountPath: "/var/run/secrets/vault.hashicorp.com/serviceaccount",
				TokenPath: "vault-token",
			},
			expectedError: "",
			pod: &corev1.Pod{
				ObjectMeta: v1.ObjectMeta{
					Annotations: map[string]string{
						"vault.hashicorp.com/agent-service-account-token-volume-name": "projected-token",
					},
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "projected-token",
							VolumeSource: corev1.VolumeSource{
								Projected: &corev1.ProjectedVolumeSource{
									Sources: []corev1.VolumeProjection{
										{
											ServiceAccountToken: &corev1.ServiceAccountTokenProjection{
												Path: "vault-token",
											},
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}
	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := serviceaccount(tc.pod)
			if len(tc.expectedError) > 0 {
				assert.EqualError(t, err, tc.expectedError)
			}
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestValidateNativeSidecar(t *testing.T) {
	baseAgent := func(native bool) Agent {
		return Agent{
			Namespace: "test",
			ServiceAccountTokenVolume: &ServiceAccountTokenVolume{
				Name:      "foobar",
				MountPath: "foobar",
				TokenPath: "foobar",
			},
			ImageName: "test",
			Vault: Vault{
				Role:     "test",
				Address:  "https://foobar.com:8200",
				AuthPath: "test",
				AuthType: "kubernetes",
			},
			NativeSidecar: native,
			PrePopulate:   true,
		}
	}

	t.Run("native base is valid", func(t *testing.T) {
		a := baseAgent(true)
		assert.NoError(t, a.Validate())
	})

	combined := func(annotation string) string {
		return AnnotationAgentNativeSidecar + " cannot be combined with " + annotation
	}
	tests := map[string]struct {
		mutate  func(a *Agent)
		wantErr string
	}{
		"pre-populate false": {
			mutate:  func(a *Agent) { a.PrePopulate = false },
			wantErr: AnnotationAgentNativeSidecar + " requires " + AnnotationAgentPrePopulate + " to be true",
		},
		"pre-populate-only true": {
			mutate:  func(a *Agent) { a.PrePopulateOnly = true },
			wantErr: combined(AnnotationAgentPrePopulateOnly),
		},
		"configmap set": {
			mutate:  func(a *Agent) { a.ConfigMapName = "cm" },
			wantErr: combined(AnnotationAgentConfigMap),
		},
		"init-json-patch set": {
			mutate:  func(a *Agent) { a.InitJsonPatch = "[]" },
			wantErr: combined(AnnotationAgentInitJsonPatch),
		},
		"inject-command set": {
			mutate: func(a *Agent) {
				a.Secrets = []*Secret{
					{Name: "cache", RawName: "cache", Path: "secret/cache"},
					{Name: "db", RawName: "db", Path: "secret/db", Command: "echo"},
				}
			},
			wantErr: combined(AnnotationAgentInjectCommand + "-db"),
		},
		"enable-quit true": {
			mutate:  func(a *Agent) { a.EnableQuit = true },
			wantErr: combined(AnnotationAgentEnableQuit),
		},
	}

	for name, tc := range tests {
		t.Run(name+" native rejected", func(t *testing.T) {
			a := baseAgent(true)
			tc.mutate(&a)
			assert.EqualError(t, a.Validate(), tc.wantErr)
		})
		t.Run(name+" non-native accepted", func(t *testing.T) {
			a := baseAgent(false)
			tc.mutate(&a)
			assert.NoError(t, a.Validate())
		})
	}
}

func buildNativePatchAgent(t *testing.T, annotations map[string]string, initContainers []corev1.Container) (*corev1.Pod, *Agent) {
	t.Helper()
	pod := goldenPod(annotations, initContainers)
	require.NoError(t, Init(pod, goldenAgentConfig()))
	agent, err := New(pod)
	require.NoError(t, err)
	require.NoError(t, agent.Validate())
	return pod, agent
}

func patchedPod(t *testing.T, annotations map[string]string, initContainers []corev1.Container) (*corev1.Pod, *Agent) {
	t.Helper()
	pod, agent := buildNativePatchAgent(t, annotations, initContainers)
	raw, err := agent.Patch()
	require.NoError(t, err)
	ops, err := jsonpatch.DecodePatch(raw)
	require.NoError(t, err)
	podJSON, err := json.Marshal(pod)
	require.NoError(t, err)
	patched, err := ops.Apply(podJSON)
	require.NoError(t, err)
	var out corev1.Pod
	require.NoError(t, json.Unmarshal(patched, &out))
	return &out, agent
}

func nativeOn(extra map[string]string) map[string]string {
	annotations := map[string]string{AnnotationAgentNativeSidecar: "true"}
	for k, v := range extra {
		annotations[k] = v
	}
	return annotations
}

func containerNames(containers []corev1.Container) []string {
	names := []string{}
	for _, c := range containers {
		names = append(names, c.Name)
	}
	return names
}

func volumeNames(volumes []corev1.Volume) []string {
	names := []string{}
	for _, v := range volumes {
		names = append(names, v.Name)
	}
	return names
}

func findContainer(t *testing.T, containers []corev1.Container, name string) corev1.Container {
	t.Helper()
	for _, c := range containers {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("container %q not found in %v", name, containerNames(containers))
	return corev1.Container{}
}

func hasSecretMount(c corev1.Container) bool {
	for _, m := range c.VolumeMounts {
		if m.Name == secretVolumeName && m.MountPath == "/vault/secrets" {
			return true
		}
	}
	return false
}

func requireUniqueMountPaths(t *testing.T, c corev1.Container) {
	t.Helper()
	seen := map[string]string{}
	for _, m := range c.VolumeMounts {
		if prev, ok := seen[m.MountPath]; ok {
			t.Fatalf("container %q mounts %q twice (volumes %q and %q)", c.Name, m.MountPath, prev, m.Name)
		}
		seen[m.MountPath] = m.Name
	}
}

func TestPatchNativeSidecar(t *testing.T) {
	t.Run("single native sidecar in initContainers", func(t *testing.T) {
		pod, _ := patchedPod(t, nativeOn(nil), nil)

		require.Equal(t, []string{"vault-agent"}, containerNames(pod.Spec.InitContainers))
		require.Equal(t, []string{"app", "other"}, containerNames(pod.Spec.Containers))
		require.Contains(t, volumeNames(pod.Spec.Volumes), "home-sidecar")
		require.NotContains(t, volumeNames(pod.Spec.Volumes), "home-init")

		agentContainer := findContainer(t, pod.Spec.InitContainers, "vault-agent")
		requireNativeShape(t, agentContainer)
		requireProbeCommand(t, agentContainer, "/home/vault/.vault-token", "/vault/secrets/db")
		require.Equal(t, false, decodedVaultConfig(t, agentContainer)["exit_after_auth"])
		require.True(t, hasSecretMount(agentContainer), "native agent must mount the secret volume it renders into")
		requireUniqueMountPaths(t, agentContainer)

		require.True(t, hasSecretMount(findContainer(t, pod.Spec.Containers, "app")), "app must get the secret volume mount")
	})

	t.Run("user init container then native sidecar", func(t *testing.T) {
		pod, _ := patchedPod(t, nativeOn(nil), setupInitContainer())

		require.Equal(t, []string{"setup", "vault-agent"}, containerNames(pod.Spec.InitContainers))
		require.Equal(t, []string{"app", "other"}, containerNames(pod.Spec.Containers))
		require.True(t, hasSecretMount(findContainer(t, pod.Spec.InitContainers, "setup")), "setup must get the secret volume mount")
		agentContainer := findContainer(t, pod.Spec.InitContainers, "vault-agent")
		requireNativeShape(t, agentContainer)
		requireUniqueMountPaths(t, agentContainer)
	})

	t.Run("init-first native sidecar before user init container", func(t *testing.T) {
		pod, _ := patchedPod(t, nativeOn(map[string]string{AnnotationAgentInitFirst: "true"}), setupInitContainer())

		require.Equal(t, []string{"vault-agent", "setup"}, containerNames(pod.Spec.InitContainers))
		require.Equal(t, []string{"app", "other"}, containerNames(pod.Spec.Containers))
		require.True(t, hasSecretMount(findContainer(t, pod.Spec.InitContainers, "setup")), "setup must get the secret volume mount")
		agentContainer := findContainer(t, pod.Spec.InitContainers, "vault-agent")
		requireNativeShape(t, agentContainer)
		requireUniqueMountPaths(t, agentContainer)
	})

	t.Run("cache enabled without persistent cache", func(t *testing.T) {
		annotations := map[string]string{AnnotationAgentCacheEnable: "true"}
		pod, agent := patchedPod(t, nativeOn(annotations), nil)
		require.False(t, agent.VaultAgentCache.Persist)
		require.NotContains(t, volumeNames(pod.Spec.Volumes), cacheVolumeName)

		agentContainer := findContainer(t, pod.Spec.InitContainers, "vault-agent")
		for _, m := range agentContainer.VolumeMounts {
			require.NotEqual(t, cacheVolumeName, m.Name)
		}
		cfg := decodedVaultConfig(t, agentContainer)
		require.NotEmpty(t, cfg["listener"], "native config must carry the cache listener")
		cache, ok := cfg["cache"].(map[string]any)
		require.True(t, ok, "native config must carry a cache stanza")
		require.Contains(t, cache, "use_auto_auth_token")
		require.NotContains(t, cache, "persist")

		todayPod, _ := patchedPod(t, annotations, nil)
		todayCfg := decodedVaultConfig(t, findContainer(t, todayPod.Spec.Containers, "vault-agent"))
		require.Equal(t, todayCfg["listener"], cfg["listener"], "listener configured as for today's sidecar")
		require.Equal(t, todayCfg["cache"].(map[string]any)["use_auto_auth_token"], cache["use_auto_auth_token"])
	})

	t.Run("shareProcessNamespace patched", func(t *testing.T) {
		pod, _ := patchedPod(t, nativeOn(map[string]string{AnnotationAgentShareProcessNamespace: "true"}), nil)
		require.NotNil(t, pod.Spec.ShareProcessNamespace)
		require.True(t, *pod.Spec.ShareProcessNamespace)
	})

	t.Run("inject-containers limits secret mounts", func(t *testing.T) {
		pod, _ := patchedPod(t, nativeOn(map[string]string{AnnotationAgentInjectContainers: "app"}), nil)
		require.True(t, hasSecretMount(findContainer(t, pod.Spec.Containers, "app")), "app must get the secret volume mount")
		require.False(t, hasSecretMount(findContainer(t, pod.Spec.Containers, "other")), "other must not get the secret volume mount")
	})

	t.Run("native false patch equals annotation absent", func(t *testing.T) {
		_, absent := buildNativePatchAgent(t, nil, nil)
		_, off := buildNativePatchAgent(t, map[string]string{AnnotationAgentNativeSidecar: "false"}, nil)
		absentPatch, err := absent.Patch()
		require.NoError(t, err)
		offPatch, err := off.Patch()
		require.NoError(t, err)
		require.Equal(t, string(normalisePatch(t, absentPatch)), string(normalisePatch(t, offPatch)))
	})
}
