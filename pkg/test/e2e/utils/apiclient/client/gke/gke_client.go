// Code generated by go-swagger; DO NOT EDIT.

package gke

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"github.com/go-openapi/runtime"
	"github.com/go-openapi/strfmt"
)

// New creates a new gke API client.
func New(transport runtime.ClientTransport, formats strfmt.Registry) ClientService {
	return &Client{transport: transport, formats: formats}
}

/*
Client for gke API
*/
type Client struct {
	transport runtime.ClientTransport
	formats   strfmt.Registry
}

// ClientOption is the option for Client methods
type ClientOption func(*runtime.ClientOperation)

// ClientService is the interface for Client methods
type ClientService interface {
	ListGKEClusterDiskTypes(params *ListGKEClusterDiskTypesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterDiskTypesOK, error)

	ListGKEClusterImages(params *ListGKEClusterImagesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterImagesOK, error)

	ListGKEClusterSizes(params *ListGKEClusterSizesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterSizesOK, error)

	ListGKEClusterZones(params *ListGKEClusterZonesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterZonesOK, error)

	ListGKEImages(params *ListGKEImagesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEImagesOK, error)

	ValidateGKECredentials(params *ValidateGKECredentialsParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ValidateGKECredentialsOK, error)

	SetTransport(transport runtime.ClientTransport)
}

/*
  ListGKEClusterDiskTypes gets g k e cluster machine disk types
*/
func (a *Client) ListGKEClusterDiskTypes(params *ListGKEClusterDiskTypesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterDiskTypesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListGKEClusterDiskTypesParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "listGKEClusterDiskTypes",
		Method:             "GET",
		PathPattern:        "/api/v2/projects/{project_id}/kubernetes/clusters/{cluster_id}/providers/gke/disktypes",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListGKEClusterDiskTypesReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListGKEClusterDiskTypesOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListGKEClusterDiskTypesDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

/*
  ListGKEClusterImages gets g k e cluster images
*/
func (a *Client) ListGKEClusterImages(params *ListGKEClusterImagesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterImagesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListGKEClusterImagesParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "listGKEClusterImages",
		Method:             "GET",
		PathPattern:        "/api/v2/projects/{project_id}/kubernetes/clusters/{cluster_id}/providers/gke/images",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListGKEClusterImagesReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListGKEClusterImagesOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListGKEClusterImagesDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

/*
  ListGKEClusterSizes gets g k e cluster machine sizes
*/
func (a *Client) ListGKEClusterSizes(params *ListGKEClusterSizesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterSizesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListGKEClusterSizesParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "listGKEClusterSizes",
		Method:             "GET",
		PathPattern:        "/api/v2/projects/{project_id}/kubernetes/clusters/{cluster_id}/providers/gke/sizes",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListGKEClusterSizesReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListGKEClusterSizesOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListGKEClusterSizesDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

/*
  ListGKEClusterZones gets g k e cluster zones
*/
func (a *Client) ListGKEClusterZones(params *ListGKEClusterZonesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEClusterZonesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListGKEClusterZonesParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "listGKEClusterZones",
		Method:             "GET",
		PathPattern:        "/api/v2/projects/{project_id}/kubernetes/clusters/{cluster_id}/providers/gke/zones",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListGKEClusterZonesReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListGKEClusterZonesOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListGKEClusterZonesDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

/*
  ListGKEImages Lists GKE image types
*/
func (a *Client) ListGKEImages(params *ListGKEImagesParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ListGKEImagesOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewListGKEImagesParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "listGKEImages",
		Method:             "GET",
		PathPattern:        "/api/v2/providers/gke/images",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ListGKEImagesReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ListGKEImagesOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ListGKEImagesDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

/*
  ValidateGKECredentials Validates GKE credentials
*/
func (a *Client) ValidateGKECredentials(params *ValidateGKECredentialsParams, authInfo runtime.ClientAuthInfoWriter, opts ...ClientOption) (*ValidateGKECredentialsOK, error) {
	// TODO: Validate the params before sending
	if params == nil {
		params = NewValidateGKECredentialsParams()
	}
	op := &runtime.ClientOperation{
		ID:                 "validateGKECredentials",
		Method:             "GET",
		PathPattern:        "/api/v2/providers/gke/validatecredentials",
		ProducesMediaTypes: []string{"application/json"},
		ConsumesMediaTypes: []string{"application/json"},
		Schemes:            []string{"https"},
		Params:             params,
		Reader:             &ValidateGKECredentialsReader{formats: a.formats},
		AuthInfo:           authInfo,
		Context:            params.Context,
		Client:             params.HTTPClient,
	}
	for _, opt := range opts {
		opt(op)
	}

	result, err := a.transport.Submit(op)
	if err != nil {
		return nil, err
	}
	success, ok := result.(*ValidateGKECredentialsOK)
	if ok {
		return success, nil
	}
	// unexpected success response
	unexpectedSuccess := result.(*ValidateGKECredentialsDefault)
	return nil, runtime.NewAPIError("unexpected success response: content available as default response in error", unexpectedSuccess, unexpectedSuccess.Code())
}

// SetTransport changes the transport on the client
func (a *Client) SetTransport(transport runtime.ClientTransport) {
	a.transport = transport
}
