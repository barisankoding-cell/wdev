package compose

import "wdev/internal/store"

// EngineInfo merangkum ketersediaan Docker Engine dan plugin Docker Compose v5 pada host.
type EngineInfo struct {
	DockerInstalled  bool   `json:"docker_installed"`
	DockerVersion    string `json:"docker_version,omitempty"`
	ComposeInstalled bool   `json:"compose_installed"`
	ComposeVersion   string `json:"compose_version,omitempty"`
	ComposeMajor     int    `json:"compose_major"`
	IsComposeV5      bool   `json:"is_compose_v5"`
	DaemonRunning    bool   `json:"daemon_running"`
	DaemonError      string `json:"daemon_error,omitempty"`
	APIVersion       string `json:"api_version,omitempty"`
	DockerRootDir    string `json:"docker_root_dir,omitempty"`
	NeedsSudo        bool   `json:"needs_sudo"`
}

// PortBinding mendeskripsikan pemetaan port host ke container dari YAML maupun runtime.
type PortBinding struct {
	Service       string `json:"service,omitempty"`
	HostIP        string `json:"host_ip"`
	HostPort      int    `json:"host_port"`
	ContainerPort int    `json:"container_port"`
	Protocol      string `json:"protocol"`
	ExposedPublic bool   `json:"exposed_public"`
	SensitivePort bool   `json:"sensitive_port"`
	Summary       string `json:"summary"`
}

// ServiceSpec mendeskripsikan definisi service atau job yang dibaca dari file Compose YAML.
type ServiceSpec struct {
	Name           string        `json:"name"`
	Image          string        `json:"image,omitempty"`
	Build          string        `json:"build,omitempty"`
	ContainerName  string        `json:"container_name,omitempty"`
	Restart        string        `json:"restart,omitempty"`
	Ports          []PortBinding `json:"ports,omitempty"`
	Profiles       []string      `json:"profiles,omitempty"`
	DependsOn      []string      `json:"depends_on,omitempty"`
	VolumesCount   int           `json:"volumes_count"`
	HasHealthcheck bool          `json:"has_healthcheck"`
	IsJob          bool          `json:"is_job,omitempty"`
	ScheduleCron   string        `json:"schedule_cron,omitempty"`
}

// ContainerStatus merepresentasikan status runtime satu container milik stack Compose.
type ContainerStatus struct {
	ID         string        `json:"id"`
	Name       string        `json:"name"`
	Service    string        `json:"service"`
	Image      string        `json:"image"`
	State      string        `json:"state"` // running | exited | paused | restarting | dead | created
	Status     string        `json:"status"`
	Health     string        `json:"health,omitempty"`
	ExitCode   int           `json:"exit_code"`
	Restarts   int           `json:"restarts,omitempty"`
	Command    string        `json:"command,omitempty"`
	Ports      []PortBinding `json:"ports,omitempty"`
	CPUPerc    string        `json:"cpu_perc,omitempty"`
	MemUsage   string        `json:"mem_usage,omitempty"`
	MemPerc    string        `json:"mem_perc,omitempty"`
	NetIO      string        `json:"net_io,omitempty"`
	BlockIO    string        `json:"block_io,omitempty"`
	PIDs       string        `json:"pids,omitempty"`
}

// LinkedVHost merangkum virtual host wdev yang terhubung ke port proyek Compose ini.
type LinkedVHost struct {
	Domain     string `json:"domain"`
	ProxyPort  int    `json:"proxy_port"`
	SSLEnabled bool   `json:"ssl_enabled"`
	ExposedTo  string `json:"exposed_to,omitempty"`
}

// Project merepresentasikan satu proyek / stack Docker Compose beserta status gabungannya.
type Project struct {
	Name             string            `json:"name"`
	DirPath          string            `json:"dir_path"`
	ComposeFile      string            `json:"compose_file"`
	ConfigFiles      []string          `json:"config_files,omitempty"`
	Description      string            `json:"description,omitempty"`
	Status           string            `json:"status"` // running | partial | exited | stopped | missing
	RawStatus        string            `json:"raw_status,omitempty"`
	Source           string            `json:"source"` // registered | discovered | both
	Registered       bool              `json:"registered"`
	FileExists       bool              `json:"file_exists"`
	HasEnvFile       bool              `json:"has_env_file"`
	ServicesCount    int               `json:"services_count"`
	RunningCount     int               `json:"running_count"`
	ActiveProfiles   []string          `json:"active_profiles,omitempty"`
	AvailableProfiles []string         `json:"available_profiles,omitempty"`
	HasJobs          bool              `json:"has_jobs"`
	ExposedPorts     []PortBinding     `json:"exposed_ports,omitempty"`
	SecurityWarnings []string          `json:"security_warnings,omitempty"`
	DefinedServices  []ServiceSpec     `json:"defined_services,omitempty"`
	Containers       []ContainerStatus `json:"containers,omitempty"`
	LinkedVHosts     []LinkedVHost     `json:"linked_vhosts,omitempty"`
	ParseError       string            `json:"parse_error,omitempty"`
	CreatedAt        string            `json:"created_at,omitempty"`
	UpdatedAt        string            `json:"updated_at,omitempty"`
}

// ProjectFiles berisi isi file Compose YAML dan file .env untuk editor UI.
type ProjectFiles struct {
	ProjectName    string   `json:"project_name"`
	DirPath        string   `json:"dir_path"`
	ComposeFile    string   `json:"compose_file"`
	ComposeContent string   `json:"compose_content"`
	EnvContent     string   `json:"env_content"`
	HasEnvFile     bool     `json:"has_env_file"`
	EnvKeys        []string `json:"env_keys,omitempty"`
}

// CreateProjectOptions mengonfigurasi pendaftaran folder lama atau pembuatan proyek Compose baru.
type CreateProjectOptions struct {
	Name           string   `json:"name"`
	DirPath        string   `json:"dir_path"`
	ComposeFile    string   `json:"compose_file,omitempty"`
	Description    string   `json:"description,omitempty"`
	Profiles       []string `json:"profiles,omitempty"`
	Template       string   `json:"template,omitempty"`
	ComposeContent string   `json:"compose_content,omitempty"`
	EnvContent     string   `json:"env_content,omitempty"`
	Overwrite      bool     `json:"overwrite,omitempty"`
	StartAfter     bool     `json:"start_after,omitempty"`
}

// ActionOptions mengatur parameter eksekusi aksi stack (up/down/pull/build/restart/run-job).
type ActionOptions struct {
	Action        string   `json:"action"` // up | down | start | stop | restart | pull | build | run-job
	Service       string   `json:"service,omitempty"`
	Profiles      []string `json:"profiles,omitempty"`
	Build         bool     `json:"build,omitempty"`
	ForceRecreate bool     `json:"force_recreate,omitempty"`
	RemoveOrphans bool     `json:"remove_orphans,omitempty"`
	RemoveVolumes bool     `json:"remove_volumes,omitempty"`
	NoCache       bool     `json:"no_cache,omitempty"`
}

// TemplateSpec mendefinisikan starter template Compose Specification v5 siap pakai.
type TemplateSpec struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	Category       string `json:"category"`
	Description    string `json:"description"`
	DefaultPort    int    `json:"default_port,omitempty"`
	ComposeContent string `json:"compose_content"`
	EnvContent     string `json:"env_content"`
}

// DiskUsageItem merangkum satu baris output `docker system df --format json`.
type DiskUsageItem struct {
	Type        string `json:"type"`
	TotalCount  string `json:"total_count"`
	Active      string `json:"active"`
	Size        string `json:"size"`
	Reclaimable string `json:"reclaimable"`
}

// DiskUsageSummary berisi daftar ringkasan pemakaian disk Docker serta daftar vhost terkait.
type DiskUsageSummary struct {
	Items []DiskUsageItem `json:"items"`
}

// StatusReport adalah respons lengkap endpoint GET /api/compose/status.
type StatusReport struct {
	Engine    EngineInfo     `json:"engine"`
	Projects  []Project      `json:"projects"`
	Templates []TemplateSpec `json:"templates"`
	Total     int            `json:"total"`
	Running   int            `json:"running"`
	Partial   int            `json:"partial"`
	Stopped   int            `json:"stopped"`
	Warnings  int            `json:"warnings"`
}

type projectRegistry interface {
	SaveComposeProject(rec store.ComposeProjectRecord) error
	ListComposeProjects() ([]store.ComposeProjectRecord, error)
	GetComposeProject(name string) (*store.ComposeProjectRecord, error)
	DeleteComposeProject(name string) error
	ListSites() ([]store.SiteRecord, error)
}
