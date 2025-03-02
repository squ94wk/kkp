// Code generated by go-swagger; DO NOT EDIT.

package project

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
)

// NewImportClusterTemplateParams creates a new ImportClusterTemplateParams object,
// with the default timeout for this client.
//
// Default values are not hydrated, since defaults are normally applied by the API server side.
//
// To enforce default values in parameter, use SetDefaults or WithDefaults.
func NewImportClusterTemplateParams() *ImportClusterTemplateParams {
	return &ImportClusterTemplateParams{
		timeout: cr.DefaultTimeout,
	}
}

// NewImportClusterTemplateParamsWithTimeout creates a new ImportClusterTemplateParams object
// with the ability to set a timeout on a request.
func NewImportClusterTemplateParamsWithTimeout(timeout time.Duration) *ImportClusterTemplateParams {
	return &ImportClusterTemplateParams{
		timeout: timeout,
	}
}

// NewImportClusterTemplateParamsWithContext creates a new ImportClusterTemplateParams object
// with the ability to set a context for a request.
func NewImportClusterTemplateParamsWithContext(ctx context.Context) *ImportClusterTemplateParams {
	return &ImportClusterTemplateParams{
		Context: ctx,
	}
}

// NewImportClusterTemplateParamsWithHTTPClient creates a new ImportClusterTemplateParams object
// with the ability to set a custom HTTPClient for a request.
func NewImportClusterTemplateParamsWithHTTPClient(client *http.Client) *ImportClusterTemplateParams {
	return &ImportClusterTemplateParams{
		HTTPClient: client,
	}
}

/* ImportClusterTemplateParams contains all the parameters to send to the API endpoint
   for the import cluster template operation.

   Typically these are written to a http.Request.
*/
type ImportClusterTemplateParams struct {

	// Body.
	Body ImportClusterTemplateBody

	// ProjectID.
	ProjectID string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithDefaults hydrates default values in the import cluster template params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *ImportClusterTemplateParams) WithDefaults() *ImportClusterTemplateParams {
	o.SetDefaults()
	return o
}

// SetDefaults hydrates default values in the import cluster template params (not the query body).
//
// All values with no default are reset to their zero value.
func (o *ImportClusterTemplateParams) SetDefaults() {
	// no default values defined for this parameter
}

// WithTimeout adds the timeout to the import cluster template params
func (o *ImportClusterTemplateParams) WithTimeout(timeout time.Duration) *ImportClusterTemplateParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the import cluster template params
func (o *ImportClusterTemplateParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the import cluster template params
func (o *ImportClusterTemplateParams) WithContext(ctx context.Context) *ImportClusterTemplateParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the import cluster template params
func (o *ImportClusterTemplateParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the import cluster template params
func (o *ImportClusterTemplateParams) WithHTTPClient(client *http.Client) *ImportClusterTemplateParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the import cluster template params
func (o *ImportClusterTemplateParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithBody adds the body to the import cluster template params
func (o *ImportClusterTemplateParams) WithBody(body ImportClusterTemplateBody) *ImportClusterTemplateParams {
	o.SetBody(body)
	return o
}

// SetBody adds the body to the import cluster template params
func (o *ImportClusterTemplateParams) SetBody(body ImportClusterTemplateBody) {
	o.Body = body
}

// WithProjectID adds the projectID to the import cluster template params
func (o *ImportClusterTemplateParams) WithProjectID(projectID string) *ImportClusterTemplateParams {
	o.SetProjectID(projectID)
	return o
}

// SetProjectID adds the projectId to the import cluster template params
func (o *ImportClusterTemplateParams) SetProjectID(projectID string) {
	o.ProjectID = projectID
}

// WriteToRequest writes these params to a swagger request
func (o *ImportClusterTemplateParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error
	if err := r.SetBodyParam(o.Body); err != nil {
		return err
	}

	// path param project_id
	if err := r.SetPathParam("project_id", o.ProjectID); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
