// Package coolify adapts the Coolify REST API to Axiom's provider-neutral
// execution boundary. It only manages prebuilt Docker images; source builds
// remain outside Coolify and must be published to a registry before Deploy.
package coolify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/digitaleflex/axiom/services/engine/internal/execution"
)

const (
	providerName    = "coolify"
	maxResponseSize = 1 << 20
)

// Config configures a self-hosted Coolify instance.
type Config struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// Client is a bounded client for the documented Coolify API.
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// APIError intentionally excludes response bodies and credentials. Provider
// responses can contain internal details and should not be copied to user logs.
type APIError struct {
	StatusCode int
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("coolify API returned HTTP %d: %s", e.StatusCode, e.Message)
}

// New validates configuration and creates a client. Plain HTTP is permitted
// only for loopback hosts (e.g. local tests); remote instances must use HTTPS.
func New(cfg Config) (*Client, error) {
	rawURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if rawURL == "" {
		return nil, errors.New("coolify base URL is required")
	}
	if strings.TrimSpace(cfg.Token) == "" {
		return nil, errors.New("coolify API token is required")
	}
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, errors.New("coolify base URL must be an absolute URL without credentials, query or fragment")
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return nil, errors.New("coolify base URL must use HTTPS")
	}
	if u.Scheme == "http" && !isLoopbackHost(u.Hostname()) {
		return nil, errors.New("coolify requires HTTPS except for loopback test instances")
	}
	basePath := strings.TrimRight(u.Path, "/")
	if !strings.HasSuffix(basePath, "/api/v1") {
		basePath += "/api/v1"
	}
	u.Path = basePath
	u.RawPath = ""
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &Client{baseURL: strings.TrimRight(u.String(), "/"), token: cfg.Token, http: cfg.HTTPClient}, nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// Health checks Coolify's public health endpoint; it does not prove that
// application deployments are healthy.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("coolify health request failed: %w", err)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &APIError{StatusCode: resp.StatusCode, Message: "health check failed"}
	}
	return nil
}

// CreateApplication provisions a Coolify Docker Image application. The image
// must already exist in a registry reachable by the Coolify deployment server.
func (c *Client) CreateApplication(ctx context.Context, spec execution.ApplicationSpec) (execution.Application, error) {
	if err := validateSpec(spec); err != nil {
		return execution.Application{}, err
	}
	body := createImageApplicationRequest{
		ProjectUUID:             spec.ProjectID,
		ServerUUID:              spec.ServerID,
		EnvironmentName:         spec.Environment,
		DockerRegistryImageName: spec.Image,
		DockerRegistryImageTag:  spec.ImageTag,
		PortsExposes:            strconv.Itoa(spec.Port),
		Name:                    spec.Name,
		Description:             spec.Description,
		InstantDeploy:           false,
		HealthCheckEnabled:      true,
		HealthCheckPath:         nonEmpty(spec.HealthPath, "/"),
		HealthCheckPort:         strconv.Itoa(spec.Port),
		HealthCheckMethod:       "GET",
		HealthCheckScheme:       "http",
		HealthCheckReturnCode:   200,
	}
	if spec.Domain != "" {
		body.Domains = spec.Domain
	}
	if spec.DestinationID != "" {
		body.DestinationUUID = spec.DestinationID
	}
	if spec.MemoryLimit != "" {
		body.MemoryLimit = spec.MemoryLimit
	}
	if spec.CPULimit != "" {
		body.CPULimit = spec.CPULimit
	}
	var response struct {
		UUID string `json:"uuid"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/applications/dockerimage", nil, body, &response); err != nil {
		return execution.Application{}, err
	}
	if response.UUID == "" {
		return execution.Application{}, errors.New("coolify created application without returning a UUID")
	}
	return execution.Application{
		Provider: providerName, ProviderID: response.UUID, Name: spec.Name,
		URL: urlForDomain(spec.Domain), RawStatus: "created",
	}, nil
}

// Deploy updates the image tag, then asks Coolify to deploy that application.
// The returned status represents queue acceptance only; callers must poll
// GetDeployment and then perform an independent external health check.
func (c *Client) Deploy(ctx context.Context, applicationID, image, tag string) (execution.Deployment, error) {
	if strings.TrimSpace(applicationID) == "" || strings.TrimSpace(image) == "" || strings.TrimSpace(tag) == "" {
		return execution.Deployment{}, errors.New("coolify application ID, image name and immutable image tag are required")
	}
	update := map[string]any{
		"docker_registry_image_name": image,
		"docker_registry_image_tag":  tag,
		"is_auto_deploy_enabled":     false,
	}
	var updated struct {
		UUID string `json:"uuid"`
	}
	if err := c.doJSON(ctx, http.MethodPatch, "/applications/"+url.PathEscape(applicationID), nil, update, &updated); err != nil {
		return execution.Deployment{}, err
	}

	query := url.Values{}
	query.Set("uuid", applicationID)
	var response struct {
		Deployments []struct {
			Message        string `json:"message"`
			ResourceUUID   string `json:"resource_uuid"`
			DeploymentUUID string `json:"deployment_uuid"`
		} `json:"deployments"`
	}
	if err := c.doJSON(ctx, http.MethodPost, "/deploy", query, nil, &response); err != nil {
		return execution.Deployment{}, err
	}
	if len(response.Deployments) == 0 || response.Deployments[0].DeploymentUUID == "" {
		return execution.Deployment{}, errors.New("coolify accepted no identifiable deployment operation")
	}
	d := response.Deployments[0]
	return execution.Deployment{
		Provider: providerName, ProviderID: d.DeploymentUUID, ApplicationID: applicationID,
		Status: execution.StatusQueued, RawStatus: "queued", Message: safeMessage(d.Message),
	}, nil
}

// GetDeployment retrieves a deployment by its Coolify deployment UUID.
// Raw provider logs are intentionally not copied into Message; callers should
// retrieve and redact logs through a dedicated, bounded log path.
func (c *Client) GetDeployment(ctx context.Context, deploymentID string) (execution.Deployment, error) {
	if strings.TrimSpace(deploymentID) == "" {
		return execution.Deployment{}, errors.New("coolify deployment ID is required")
	}
	var response deploymentResponse
	if err := c.doJSON(ctx, http.MethodGet, "/deployments/"+url.PathEscape(deploymentID), nil, nil, &response); err != nil {
		return execution.Deployment{}, err
	}
	return execution.Deployment{
		Provider: providerName, ProviderID: response.DeploymentUUID,
		ApplicationID: response.ApplicationID, Status: normalizeStatus(response.Status),
		RawStatus: response.Status, URL: response.DeploymentURL,
		CreatedAt: parseTime(response.CreatedAt), UpdatedAt: parseTime(response.UpdatedAt),
	}, nil
}

// GetApplication returns basic runtime metadata for an existing Coolify app.
func (c *Client) GetApplication(ctx context.Context, applicationID string) (execution.Application, error) {
	if strings.TrimSpace(applicationID) == "" {
		return execution.Application{}, errors.New("coolify application ID is required")
	}
	var response applicationResponse
	if err := c.doJSON(ctx, http.MethodGet, "/applications/"+url.PathEscape(applicationID), nil, nil, &response); err != nil {
		return execution.Application{}, err
	}
	return execution.Application{
		Provider: providerName, ProviderID: response.UUID, Name: response.Name,
		URL: firstDomain(response.FQDN, response.Domains), RawStatus: response.Status,
	}, nil
}

type createImageApplicationRequest struct {
	ProjectUUID             string `json:"project_uuid"`
	ServerUUID              string `json:"server_uuid"`
	EnvironmentName         string `json:"environment_name"`
	DockerRegistryImageName string `json:"docker_registry_image_name"`
	DockerRegistryImageTag  string `json:"docker_registry_image_tag"`
	PortsExposes            string `json:"ports_exposes"`
	Name                    string `json:"name,omitempty"`
	Description             string `json:"description,omitempty"`
	Domains                 string `json:"domains,omitempty"`
	DestinationUUID         string `json:"destination_uuid,omitempty"`
	InstantDeploy            bool   `json:"instant_deploy"`
	HealthCheckEnabled       bool   `json:"health_check_enabled"`
	HealthCheckPath          string `json:"health_check_path"`
	HealthCheckPort          string `json:"health_check_port"`
	HealthCheckMethod        string `json:"health_check_method"`
	HealthCheckScheme        string `json:"health_check_scheme"`
	HealthCheckReturnCode    int    `json:"health_check_return_code"`
	MemoryLimit              string `json:"limits_memory,omitempty"`
	CPULimit                 string `json:"limits_cpus,omitempty"`
}

type deploymentResponse struct {
	DeploymentUUID string `json:"deployment_uuid"`
	ApplicationID  string `json:"application_id"`
	Status         string `json:"status"`
	DeploymentURL  string `json:"deployment_url"`
	CreatedAt      string `json:"created_at"`
	UpdatedAt      string `json:"updated_at"`
}

type applicationResponse struct {
	UUID    string `json:"uuid"`
	Name    string `json:"name"`
	FQDN    string `json:"fqdn"`
	Domains string `json:"domains"`
	Status  string `json:"status"`
}

func (c *Client) doJSON(ctx context.Context, method, path string, query url.Values, body, out any) error {
	endpoint := c.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Coolify request: %w", err)
		}
		reader = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return fmt.Errorf("create Coolify request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("Coolify request failed: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseSize+1))
	if err != nil {
		return fmt.Errorf("read Coolify response: %w", err)
	}
	if len(data) > maxResponseSize {
		return errors.New("Coolify response exceeded the 1 MiB limit")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		message := "request rejected"
		var payload struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &payload) == nil && payload.Message != "" {
			message = safeMessage(payload.Message)
		}
		return &APIError{StatusCode: resp.StatusCode, Message: message}
	}
	if out == nil || len(data) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode Coolify response: %w", err)
	}
	return nil
}

func validateSpec(spec execution.ApplicationSpec) error {
	switch {
	case strings.TrimSpace(spec.Name) == "":
		return errors.New("application name is required")
	case strings.TrimSpace(spec.Image) == "":
		return errors.New("registry image name is required")
	case strings.TrimSpace(spec.ImageTag) == "":
		return errors.New("immutable registry image tag is required")
	case strings.TrimSpace(spec.ProjectID) == "":
		return errors.New("Coolify project UUID is required")
	case strings.TrimSpace(spec.ServerID) == "":
		return errors.New("Coolify server UUID is required")
	case strings.TrimSpace(spec.Environment) == "":
		return errors.New("Coolify environment name is required")
	case spec.Port < 1 || spec.Port > 65535:
		return errors.New("application port must be between 1 and 65535")
	}
	return nil
}

func normalizeStatus(raw string) execution.Status {
	s := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(s, "cancel"):
		return execution.StatusCancelled
	case strings.Contains(s, "fail"), strings.Contains(s, "error"):
		return execution.StatusFailed
	case strings.Contains(s, "queue"), strings.Contains(s, "pending"):
		return execution.StatusQueued
	case strings.Contains(s, "build"), strings.Contains(s, "install"), strings.Contains(s, "clone"):
		return execution.StatusBuilding
	case strings.Contains(s, "deploy"), strings.Contains(s, "running"), strings.Contains(s, "in progress"):
		return execution.StatusDeploying
	case strings.Contains(s, "finished"), strings.Contains(s, "success"), strings.Contains(s, "completed"):
		return execution.StatusSucceeded
	default:
		return execution.StatusUnknown
	}
}

func safeMessage(value string) string {
	value = strings.TrimSpace(value)
	if len(value) > 500 {
		value = value[:500]
	}
	return value
}

func nonEmpty(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func urlForDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}
	if strings.HasPrefix(domain, "http://") || strings.HasPrefix(domain, "https://") {
		return domain
	}
	return "https://" + domain
}

func firstDomain(fqdn, domains string) string {
	if strings.TrimSpace(fqdn) != "" {
		return urlForDomain(strings.Split(fqdn, ",")[0])
	}
	if strings.TrimSpace(domains) != "" {
		return urlForDomain(strings.Split(domains, ",")[0])
	}
	return ""
}

func parseTime(value string) time.Time {
	t, _ := time.Parse(time.RFC3339, value)
	return t
}
