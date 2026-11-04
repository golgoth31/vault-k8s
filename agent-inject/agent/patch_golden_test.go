// Copyright (c) HashiCorp, Inc.
// SPDX-License-Identifier: MPL-2.0

package agent

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var updateGolden = flag.Bool("update-golden", false, "rewrite testdata/patch-golden fixtures")

var customSecretVolumeName = regexp.MustCompile(`^vault-secrets-custom-\d+$`)

func goldenAgentConfig() AgentConfig {
	cfg := basicAgentConfig()
	cfg.RevokeOnShutdown = false
	return cfg
}

func goldenPod(annotations map[string]string, initContainers []corev1.Container) *corev1.Pod {
	base := map[string]string{
		AnnotationAgentInject:               "true",
		AnnotationVaultRole:                 "app",
		AnnotationAgentInjectSecret + "-db": "secret/data/db",
	}
	for k, v := range annotations {
		base[k] = v
	}
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:        "golden",
			Namespace:   "test",
			Annotations: base,
		},
		Spec: corev1.PodSpec{
			Volumes: []corev1.Volume{
				{
					Name: "kube-api-access",
					VolumeSource: corev1.VolumeSource{
						Projected: &corev1.ProjectedVolumeSource{
							Sources: []corev1.VolumeProjection{
								{ServiceAccountToken: &corev1.ServiceAccountTokenProjection{Path: "token"}},
							},
						},
					},
				},
			},
			InitContainers: initContainers,
			Containers: []corev1.Container{
				{
					Name:  "app",
					Image: "app-image",
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "kube-api-access",
							MountPath: "/var/run/secrets/kubernetes.io/serviceaccount",
							ReadOnly:  true,
						},
					},
				},
				{
					Name:  "other",
					Image: "other-image",
				},
			},
		},
	}
}

func setupInitContainer() []corev1.Container {
	return []corev1.Container{
		{
			Name:  "setup",
			Image: "setup-image",
			VolumeMounts: []corev1.VolumeMount{
				{Name: "kube-api-access", MountPath: "/var/run/secrets/kubernetes.io/serviceaccount", ReadOnly: true},
			},
		},
	}
}

func TestPatchGolden(t *testing.T) {
	tests := []struct {
		name           string
		annotations    map[string]string
		initContainers []corev1.Container
	}{
		{name: "default"},
		{
			name:           "init-first",
			annotations:    map[string]string{AnnotationAgentInitFirst: "true"},
			initContainers: setupInitContainer(),
		},
		{
			name:           "user-init-container",
			initContainers: setupInitContainer(),
		},
		{
			name:        "pre-populate-false",
			annotations: map[string]string{AnnotationAgentPrePopulate: "false"},
		},
		{
			name:        "pre-populate-only",
			annotations: map[string]string{AnnotationAgentPrePopulateOnly: "true"},
		},
		{
			name:        "cache-enable",
			annotations: map[string]string{AnnotationAgentCacheEnable: "true"},
		},
		{
			name:        "inject-token",
			annotations: map[string]string{AnnotationAgentInjectToken: "true"},
		},
		{
			name:        "revoke-on-shutdown",
			annotations: map[string]string{AnnotationAgentRevokeOnShutdown: "true"},
		},
		{
			name: "json-patch",
			annotations: map[string]string{
				AnnotationAgentJsonPatch:     `[{"op":"add","path":"/workingDir","value":"/tmp/sidecar"}]`,
				AnnotationAgentInitJsonPatch: `[{"op":"add","path":"/workingDir","value":"/tmp/init"}]`,
			},
		},
		{
			name:        "configmap",
			annotations: map[string]string{AnnotationAgentConfigMap: "agent-config"},
		},
		{
			name:        "share-process-namespace",
			annotations: map[string]string{AnnotationAgentShareProcessNamespace: "true"},
		},
		{
			name:        "inject-containers",
			annotations: map[string]string{AnnotationAgentInjectContainers: "app"},
		},
		{
			name: "two-secrets-custom-paths",
			annotations: map[string]string{
				AnnotationAgentInjectSecret + "-cache":     "secret/data/cache",
				AnnotationVaultSecretVolumePath + "-db":    "/etc/db",
				AnnotationVaultSecretVolumePath + "-cache": "/etc/cache",
			},
		},
		{
			name:        "native-sidecar",
			annotations: map[string]string{AnnotationAgentNativeSidecar: "true"},
		},
		{
			name: "native-sidecar-init-first",
			annotations: map[string]string{
				AnnotationAgentNativeSidecar: "true",
				AnnotationAgentInitFirst:     "true",
			},
			initContainers: setupInitContainer(),
		},
		{
			name:           "native-sidecar-user-init-container",
			annotations:    map[string]string{AnnotationAgentNativeSidecar: "true"},
			initContainers: setupInitContainer(),
		},
	}

	for _, tt := range tests {
		t.Run("AC1_"+tt.name, func(t *testing.T) {
			pod := goldenPod(tt.annotations, tt.initContainers)
			require.NoError(t, Init(pod, goldenAgentConfig()))
			agent, err := New(pod)
			require.NoError(t, err)
			require.NoError(t, agent.Validate())
			patch, err := agent.Patch()
			require.NoError(t, err)

			got := normalisePatch(t, patch)
			fixture := filepath.Join("testdata", "patch-golden", tt.name+".json")
			if *updateGolden {
				require.NoError(t, os.MkdirAll(filepath.Dir(fixture), 0o755))
				require.NoError(t, os.WriteFile(fixture, got, 0o644))
				return
			}
			want, err := os.ReadFile(fixture)
			require.NoError(t, err, "missing fixture: run go test -run TestPatchGolden -update-golden")
			require.Equal(t, string(want), string(got))
		})
	}
}

// normalisePatch renders a Patch() result so that the order of a.Secrets, which comes
// from map iteration in New, does not change the bytes.
func normalisePatch(t *testing.T, raw []byte) []byte {
	t.Helper()
	doc := decodeJSON(t, raw)
	doc = decodeVaultConfigs(t, doc)

	paths := map[string]string{}
	collectCustomVolumePaths(t, doc, paths)
	unique := map[string]bool{}
	for _, p := range paths {
		unique[p] = true
	}
	require.Len(t, unique, len(paths), "two custom secret volumes share one mountPath")
	sorted := make([]string, 0, len(unique))
	for p := range unique {
		sorted = append(sorted, p)
	}
	sort.Strings(sorted)
	rename := map[string]string{}
	for name, p := range paths {
		rename[name] = fmt.Sprintf("vault-secrets-custom-%d", sort.SearchStrings(sorted, p))
	}
	doc = renameCustomVolumes(doc, rename)
	doc = sortCustomVolumeRuns(doc)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	require.NoError(t, enc.Encode(doc))
	return buf.Bytes()
}

func decodeJSON(t *testing.T, raw []byte) any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var doc any
	require.NoError(t, dec.Decode(&doc))
	return doc
}

func decodeVaultConfigs(t *testing.T, node any) any {
	switch v := node.(type) {
	case map[string]any:
		if v["name"] == "VAULT_CONFIG" {
			if s, ok := v["value"].(string); ok {
				decoded, err := base64.StdEncoding.DecodeString(s)
				require.NoError(t, err)
				cfg := decodeJSON(t, decoded)
				if m, ok := cfg.(map[string]any); ok {
					if tmpls, ok := m["template"].([]any); ok {
						sort.SliceStable(tmpls, func(i, j int) bool {
							return fmt.Sprint(tmpls[i].(map[string]any)["destination"]) < fmt.Sprint(tmpls[j].(map[string]any)["destination"])
						})
					}
				}
				v["value"] = cfg
				return v
			}
		}
		for k, child := range v {
			v[k] = decodeVaultConfigs(t, child)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = decodeVaultConfigs(t, child)
		}
		return v
	default:
		return node
	}
}

func collectCustomVolumePaths(t *testing.T, node any, paths map[string]string) {
	switch v := node.(type) {
	case map[string]any:
		name, _ := v["name"].(string)
		mountPath, hasPath := v["mountPath"].(string)
		if hasPath && customSecretVolumeName.MatchString(name) {
			if prev, ok := paths[name]; ok && prev != mountPath {
				t.Fatalf("volume %s mounted at both %s and %s", name, prev, mountPath)
			}
			paths[name] = mountPath
		}
		for _, child := range v {
			collectCustomVolumePaths(t, child, paths)
		}
	case []any:
		for _, child := range v {
			collectCustomVolumePaths(t, child, paths)
		}
	}
}

func renameCustomVolumes(node any, rename map[string]string) any {
	switch v := node.(type) {
	case map[string]any:
		if name, ok := v["name"].(string); ok {
			if to, ok := rename[name]; ok {
				v["name"] = to
			}
		}
		for k, child := range v {
			v[k] = renameCustomVolumes(child, rename)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = renameCustomVolumes(child, rename)
		}
		return v
	default:
		return node
	}
}

func customVolumeSortKey(elem any) (group, name string) {
	m, ok := elem.(map[string]any)
	if !ok {
		return "", ""
	}
	if n, ok := m["name"].(string); ok && customSecretVolumeName.MatchString(n) {
		return "", n
	}
	if op, ok := m["path"].(string); ok {
		if value, ok := m["value"].(map[string]any); ok {
			if n, ok := value["name"].(string); ok && customSecretVolumeName.MatchString(n) {
				return op, n
			}
		}
	}
	return "", ""
}

func sortCustomVolumeRuns(node any) any {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			v[k] = sortCustomVolumeRuns(child)
		}
		return v
	case []any:
		for i, child := range v {
			v[i] = sortCustomVolumeRuns(child)
		}
		for start := 0; start < len(v); {
			group, name := customVolumeSortKey(v[start])
			if name == "" {
				start++
				continue
			}
			end := start + 1
			for end < len(v) {
				g, n := customVolumeSortKey(v[end])
				if n == "" || g != group {
					break
				}
				end++
			}
			run := v[start:end]
			sort.SliceStable(run, func(i, j int) bool {
				_, a := customVolumeSortKey(run[i])
				_, b := customVolumeSortKey(run[j])
				return a < b
			})
			start = end
		}
		return v
	default:
		return node
	}
}
