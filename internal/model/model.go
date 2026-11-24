// Package model defines the application-owned types shared by the Compose
// reader, the Docker inspector, the drift engine and the reporters. It imports
// no other project package.
package model

// PortBinding is one published port. All fields are comparable, so values
// can be compared with ==.
type PortBinding struct {
	HostIP        string // empty means "not specified" (all interfaces)
	HostPort      string // empty means Docker chooses a free port
	ContainerPort int
	Protocol      string // "tcp", "udp" or "sctp"
}

// Mount is one volume or bind mount.
type Mount struct {
	Type     string // "bind" or "volume"
	Source   string // host path for bind, resolved name for volume, empty if anonymous
	Target   string // path inside the container
	ReadOnly bool
}

// RestartPolicy mirrors Docker's restart policy.
type RestartPolicy struct {
	Name       string // "no", "always", "unless-stopped" or "on-failure"
	MaxRetries int
}

// ServiceSpec is the desired configuration of one Compose service.
type ServiceSpec struct {
	Name       string
	Image      string // empty when the service is built from source
	HasBuild   bool
	Ports      []PortBinding
	Mounts     []Mount
	Restart    RestartPolicy
	Privileged bool
}

// Project is the desired state of a whole Compose project.
type Project struct {
	Name     string
	Services []ServiceSpec // sorted by name
	Warnings []string      // features that were skipped, not drift
}

// ContainerState is the observed configuration of one container that belongs
// to a Compose project. Runtime-only values (IP addresses, timestamps, health)
// are deliberately not part of it.
type ContainerState struct {
	ID         string
	Name       string
	Service    string
	State      string // "running", "exited", "created", ...
	OneOff     bool   // created by "docker compose run"
	Image      string // image reference as given when the container was created
	Ports      []PortBinding
	Mounts     []Mount
	Restart    RestartPolicy
	Privileged bool
}
