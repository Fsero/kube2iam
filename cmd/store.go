package cmd

import (
	"fmt"
	"sync"

	log "github.com/Sirupsen/logrus"
	"k8s.io/kubernetes/pkg/api"
)

// store implements the k8s framework ResourceEventHandler interface.
type store struct {
	defaultRole          string
	iamRoleKey           string
	namespaceKey         string
	namespaceRestriction bool
	mutex                sync.RWMutex
	rolesByIP            map[string]string
	rolesByNamespace     map[string][]string
	namespaceByIP        map[string]string
	iam                  *iam
}

// Get returns the iam role based on IP address.
func (s *store) Get(IP string) (string, error) {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	if role, ok := s.rolesByIP[IP]; ok {
		return role, nil
	}
	if s.defaultRole != "" {
		log.Warnf("Using fallback role for IP %s", IP)
		return s.defaultRole, nil
	}
	return "", fmt.Errorf("Unable to find role for IP %s", IP)
}

func (s *store) AddRoleToIP(pod *api.Pod, role string) {
	s.mutex.Lock()
	s.rolesByIP[pod.Status.PodIP] = role
	s.mutex.Unlock()
}

func (s *store) AddNamespaceToIP(pod *api.Pod) {
	namespace := pod.GetNamespace()
	s.mutex.Lock()
	s.namespaceByIP[pod.Status.PodIP] = namespace
	s.mutex.Unlock()
}

func (s *store) DeleteIP(ip string) {
	s.mutex.Lock()
	delete(s.rolesByIP, ip)
	delete(s.namespaceByIP, ip)
	s.mutex.Unlock()
}

// AddRoleToNamespace takes a role name and adds it to our internal state
func (s *store) AddRoleToNamespace(namespace string, role string) {
	// Make sure to add the full ARN of roles to ensure string matching works
	roleARN := s.iam.roleARN(role)

	ar := s.rolesByNamespace[namespace]

	// this is a tiny bit troubling, we could go with a the rolesByNamespace
	// being a map[string]map[string]bool so that deduplication isn't
	// ever a problem .. but for now...
	c := true
	for i := range ar {
		if ar[i] == roleARN {
			c = false
			break
		}
	}
	if c {
		ar = append(ar, roleARN)
	}
	s.mutex.Lock()
	s.rolesByNamespace[namespace] = ar
	s.mutex.Unlock()
}

// RemoveRoleFromNamespace takes a role and removes it from a namespace mapping
func (s *store) RemoveRoleFromNamespace(namespace string, role string) {
	// Make sure to remove the full ARN of roles to ensure string matching works
	roleARN := s.iam.roleARN(role)

	ar := s.rolesByNamespace[namespace]
	for i := range ar {
		if ar[i] == roleARN {
			ar = append(ar[:i], ar[i+1:]...)
			break
		}
	}
	s.mutex.Lock()
	s.rolesByNamespace[namespace] = ar
	s.mutex.Unlock()
}

// DeleteNamespace removes all role mappings from a namespace
func (s *store) DeleteNamespace(namespace string) {
	s.mutex.Lock()
	delete(s.rolesByNamespace, namespace)
	s.mutex.Unlock()
}

// checkRoleForNamespace checks the 'database' for a role allowed in a namespace,
// returns true if the role is found, otheriwse false
func (s *store) checkRoleForNamespace(role string, namespace string) bool {
	ar := s.rolesByNamespace[namespace]
	for _, r := range ar {
		if r == role {
			log.Debugf("Role:%s on namespace:%s found.", role, namespace)
			return true
		}
	}
	log.Warnf("Role:%s on namespace:%s not found.", role, namespace)
	return false
}

func (s *store) CheckNamespaceRestriction(role string, ip string) (bool, string) {
	ns := s.namespaceByIP[ip]

	// if the namespace restrictions are not in place early out true
	if !s.namespaceRestriction {
		return true, ns
	}

	// if the role is the default role you are also good
	if role == s.iam.roleARN(s.defaultRole) {
		return true, ns
	}

	return s.checkRoleForNamespace(role, ns), ns
}

func (s *store) DumpRolesByIP() map[string]string {
	return s.rolesByIP
}

func (s *store) DumpRolesByNamespace() map[string][]string {
	return s.rolesByNamespace
}

func (s *store) DumpNamespaceByIP() map[string]string {
	return s.namespaceByIP
}

func newStore(key string, defaultRole string, namespaceRestriction bool, namespaceKey string, iamInstance *iam) *store {
	return &store{
		defaultRole:          defaultRole,
		iamRoleKey:           key,
		namespaceKey:         namespaceKey,
		namespaceRestriction: namespaceRestriction,
		rolesByIP:            make(map[string]string),
		rolesByNamespace:     make(map[string][]string),
		namespaceByIP:        make(map[string]string),
		iam:                  iamInstance,
	}
}
