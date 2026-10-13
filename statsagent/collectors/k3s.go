package collectors

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"
)

// K3sStats holds k3s/kubernetes cluster metrics visible from this node.
type K3sStats struct {
	Timestamp   time.Time   `json:"timestamp"`
	Available   bool        `json:"available"`
	NodeName    string      `json:"node_name"`
	TotalNodes  int         `json:"total_nodes"`
	ReadyNodes  int         `json:"ready_nodes"`
	TotalPods   int         `json:"total_pods"`
	RunningPods int         `json:"running_pods"`
	FailedPods  int         `json:"failed_pods"`
	Nodes       []NodeInfo  `json:"nodes"`
	Namespaces  []NSInfo    `json:"namespaces"`
}

// NodeInfo holds information about a k3s node.
type NodeInfo struct {
	Name    string `json:"name"`
	Role    string `json:"role"`
	Status  string `json:"status"`
	Version string `json:"version"`
}

// NSInfo holds per-namespace pod counts.
type NSInfo struct {
	Name        string `json:"name"`
	TotalPods   int    `json:"total_pods"`
	RunningPods int    `json:"running_pods"`
}

// k3sClient calls the Kubernetes API using a service account or kubeconfig token.
type k3sClient struct {
	httpClient *http.Client
	apiBase    string
	token      string
}

func newK3sClient(kubeconfig string) (*k3sClient, error) {
	// Try in-cluster first (when running as a k3s pod)
	if _, err := os.Stat("/var/run/secrets/kubernetes.io/serviceaccount/token"); err == nil {
		token, err := os.ReadFile("/var/run/secrets/kubernetes.io/serviceaccount/token")
		if err != nil {
			return nil, fmt.Errorf("read service account token: %w", err)
		}
		caPool, err := loadCA("/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
		if err != nil {
			return nil, err
		}
		return &k3sClient{
			httpClient: tlsClient(caPool),
			apiBase:    "https://kubernetes.default.svc",
			token:      string(token),
		}, nil
	}

	// Fall back to kubeconfig file
	if kubeconfig == "" {
		if home, err := os.UserHomeDir(); err == nil {
			kubeconfig = home + "/.kube/config"
		}
	}

	// k3s default kubeconfig
	if _, err := os.Stat(kubeconfig); err != nil {
		kubeconfig = "/etc/rancher/k3s/k3s.yaml"
	}

	if _, err := os.Stat(kubeconfig); err != nil {
		return nil, fmt.Errorf("no kubeconfig found")
	}

	server, token, ca, err := parseMinimalKubeconfig(kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("parse kubeconfig: %w", err)
	}

	var caPool interface{ SystemCertPool() interface{} }
	_ = caPool

	pool, err := loadCA(ca)
	if err != nil {
		return nil, fmt.Errorf("load kubeconfig CA: %w", err)
	}
	return &k3sClient{
		httpClient: tlsClient(pool),
		apiBase:    server,
		token:      token,
	}, nil
}

func (c *k3sClient) get(path string, out interface{}) error {
	req, err := http.NewRequest("GET", c.apiBase+path, nil)
	if err != nil {
		return err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("API returned %d: %s", resp.StatusCode, string(body))
	}

	return json.NewDecoder(resp.Body).Decode(out)
}

// CollectK3s collects cluster state via the Kubernetes API.
// Returns an unavailable K3sStats if k3s is not reachable.
func CollectK3s(kubeconfig string) (*K3sStats, error) {
	s := &K3sStats{Timestamp: time.Now()}
	hostname, _ := os.Hostname()
	s.NodeName = hostname

	client, err := newK3sClient(kubeconfig)
	if err != nil {
		s.Available = false
		return s, nil
	}

	// Check connectivity
	var versionResp map[string]interface{}
	if err := client.get("/version", &versionResp); err != nil {
		s.Available = false
		return s, nil
	}
	s.Available = true

	// Nodes
	var nodeList struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
				} `json:"conditions"`
				NodeInfo struct {
					KubeletVersion string `json:"kubeletVersion"`
				} `json:"nodeInfo"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := client.get("/api/v1/nodes", &nodeList); err == nil {
		s.TotalNodes = len(nodeList.Items)
		for _, n := range nodeList.Items {
			ni := NodeInfo{
				Name:    n.Metadata.Name,
				Version: n.Status.NodeInfo.KubeletVersion,
			}
			// Determine role
			if _, ok := n.Metadata.Labels["node-role.kubernetes.io/master"]; ok {
				ni.Role = "master"
			} else if _, ok := n.Metadata.Labels["node-role.kubernetes.io/control-plane"]; ok {
				ni.Role = "control-plane"
			} else {
				ni.Role = "worker"
			}
			// Check ready condition
			for _, cond := range n.Status.Conditions {
				if cond.Type == "Ready" {
					ni.Status = cond.Status
					if cond.Status == "True" {
						s.ReadyNodes++
					}
					break
				}
			}
			s.Nodes = append(s.Nodes, ni)
		}
	}

	// Pods (all namespaces)
	var podList struct {
		Items []struct {
			Metadata struct {
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}

	if err := client.get("/api/v1/pods", &podList); err == nil {
		s.TotalPods = len(podList.Items)
		nsCounts := make(map[string]*NSInfo)

		for _, p := range podList.Items {
			ns := p.Metadata.Namespace
			if nsCounts[ns] == nil {
				nsCounts[ns] = &NSInfo{Name: ns}
			}
			nsCounts[ns].TotalPods++

			switch p.Status.Phase {
			case "Running":
				s.RunningPods++
				nsCounts[ns].RunningPods++
			case "Failed":
				s.FailedPods++
			}
		}

		for _, ns := range nsCounts {
			s.Namespaces = append(s.Namespaces, *ns)
		}
	}

	return s, nil
}
