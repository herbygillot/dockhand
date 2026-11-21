package gitlab

import (
	"bytes"
	"net/http"
	"time"
)

var (
	routeProjectsIDAttestationsID                  = route("projects/%s/attestations/%s")
	routeProjectsIDAttestationsIDDownload          = route("projects/%s/attestations/%d/download")
	routeProjectsIDAttestationsSigningCertificates = route("projects/%s/attestations/signing_certificates")
)

type (
	AttestationsServiceInterface interface {
		// ListAttestations gets a list of all attestations
		//
		// GitLab API docs: https://docs.gitlab.com/api/attestations/#list-all-attestations
		ListAttestations(pid any, subjectDigest string, options ...RequestOptionFunc) ([]*Attestation, *Response, error)

		// DownloadAttestation
		//
		// GitLab API docs: https://docs.gitlab.com/api/attestations/#download-an-attestation
		DownloadAttestation(pid any, attestationIID int64, options ...RequestOptionFunc) ([]byte, *Response, error)

		// ListSigningCertificates gets a list of the unexpired certificate
		// authority (CA) certificates of a project, active and inactive.
		// GitLab uses these CAs for the LOCAL_CA attestation method.
		//
		// GitLab API docs: https://docs.gitlab.com/api/attestations/#list-all-signing-certificates
		ListSigningCertificates(pid any, opt *ListSigningCertificatesOptions, options ...RequestOptionFunc) ([]*SigningCertificate, *Response, error)
	}

	// AttestationsService handles communication with the keys related methods
	// of the GitLab API.
	//
	// GitLab API docs: https://docs.gitlab.com/api/attestations/
	AttestationsService struct {
		client *Client
	}
)

var _ AttestationsServiceInterface = (*AttestationsService)(nil)

type Attestation struct {
	ID            int64      `json:"id"`
	IID           int64      `json:"iid"`
	ProjectID     int64      `json:"project_id"`
	BuildID       int64      `json:"build_id"`
	Status        string     `json:"status"`
	CreatedAt     *time.Time `json:"created_at"`
	UpdatedAt     *time.Time `json:"updated_at"`
	ExpireAt      *time.Time `json:"expire_at"`
	PredicateKind string     `json:"predicate_kind"`
	PredicateType string     `json:"predicate_type"`
	SubjectDigest string     `json:"subject_digest"`
	SigningMethod string     `json:"signing_method"`
	DownloadURL   string     `json:"download_url"`
}

// SigningCertificate represents a certificate authority (CA) certificate
// that GitLab uses for the LOCAL_CA attestation method.
//
// GitLab API docs: https://docs.gitlab.com/api/attestations/#list-all-signing-certificates
type SigningCertificate struct {
	ID          int64      `json:"id"`
	Certificate string     `json:"certificate"`
	Active      bool       `json:"active"`
	ExpiresAt   *time.Time `json:"expires_at"`
	CreatedAt   *time.Time `json:"created_at"`
}

func (s *AttestationsService) ListAttestations(pid any, subjectDigest string, options ...RequestOptionFunc) ([]*Attestation, *Response, error) {
	return do[[]*Attestation](s.client,
		withMethod(http.MethodGet),
		withPath(routeProjectsIDAttestationsID, ProjectID{pid}, subjectDigest),
		withRequestOpts(options...),
	)
}

func (s *AttestationsService) DownloadAttestation(pid any, attestationIID int64, options ...RequestOptionFunc) ([]byte, *Response, error) {
	b, resp, err := do[bytes.Buffer](s.client,
		withMethod(http.MethodGet),
		withPath(routeProjectsIDAttestationsIDDownload, ProjectID{pid}, attestationIID),
		withRequestOpts(options...),
	)

	return b.Bytes(), resp, err
}

// ListSigningCertificatesOptions represents the available
// ListSigningCertificates() options.
//
// GitLab API docs: https://docs.gitlab.com/api/attestations/#list-all-signing-certificates
type ListSigningCertificatesOptions struct {
	ListOptions
}

func (s *AttestationsService) ListSigningCertificates(pid any, opt *ListSigningCertificatesOptions, options ...RequestOptionFunc) ([]*SigningCertificate, *Response, error) {
	return do[[]*SigningCertificate](s.client,
		withMethod(http.MethodGet),
		withPath(routeProjectsIDAttestationsSigningCertificates, ProjectID{pid}),
		withAPIOpts(opt),
		withRequestOpts(options...),
	)
}
