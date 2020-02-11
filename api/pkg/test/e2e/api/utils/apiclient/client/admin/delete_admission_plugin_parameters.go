// Code generated by go-swagger; DO NOT EDIT.

package admin

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"context"
	"net/http"
	"time"

	"github.com/go-openapi/errors"
	"github.com/go-openapi/runtime"
	cr "github.com/go-openapi/runtime/client"

	strfmt "github.com/go-openapi/strfmt"
)

// NewDeleteAdmissionPluginParams creates a new DeleteAdmissionPluginParams object
// with the default values initialized.
func NewDeleteAdmissionPluginParams() *DeleteAdmissionPluginParams {
	var ()
	return &DeleteAdmissionPluginParams{

		timeout: cr.DefaultTimeout,
	}
}

// NewDeleteAdmissionPluginParamsWithTimeout creates a new DeleteAdmissionPluginParams object
// with the default values initialized, and the ability to set a timeout on a request
func NewDeleteAdmissionPluginParamsWithTimeout(timeout time.Duration) *DeleteAdmissionPluginParams {
	var ()
	return &DeleteAdmissionPluginParams{

		timeout: timeout,
	}
}

// NewDeleteAdmissionPluginParamsWithContext creates a new DeleteAdmissionPluginParams object
// with the default values initialized, and the ability to set a context for a request
func NewDeleteAdmissionPluginParamsWithContext(ctx context.Context) *DeleteAdmissionPluginParams {
	var ()
	return &DeleteAdmissionPluginParams{

		Context: ctx,
	}
}

// NewDeleteAdmissionPluginParamsWithHTTPClient creates a new DeleteAdmissionPluginParams object
// with the default values initialized, and the ability to set a custom HTTPClient for a request
func NewDeleteAdmissionPluginParamsWithHTTPClient(client *http.Client) *DeleteAdmissionPluginParams {
	var ()
	return &DeleteAdmissionPluginParams{
		HTTPClient: client,
	}
}

/*DeleteAdmissionPluginParams contains all the parameters to send to the API endpoint
for the delete admission plugin operation typically these are written to a http.Request
*/
type DeleteAdmissionPluginParams struct {

	/*Name*/
	Name string

	timeout    time.Duration
	Context    context.Context
	HTTPClient *http.Client
}

// WithTimeout adds the timeout to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) WithTimeout(timeout time.Duration) *DeleteAdmissionPluginParams {
	o.SetTimeout(timeout)
	return o
}

// SetTimeout adds the timeout to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) SetTimeout(timeout time.Duration) {
	o.timeout = timeout
}

// WithContext adds the context to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) WithContext(ctx context.Context) *DeleteAdmissionPluginParams {
	o.SetContext(ctx)
	return o
}

// SetContext adds the context to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) SetContext(ctx context.Context) {
	o.Context = ctx
}

// WithHTTPClient adds the HTTPClient to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) WithHTTPClient(client *http.Client) *DeleteAdmissionPluginParams {
	o.SetHTTPClient(client)
	return o
}

// SetHTTPClient adds the HTTPClient to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) SetHTTPClient(client *http.Client) {
	o.HTTPClient = client
}

// WithName adds the name to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) WithName(name string) *DeleteAdmissionPluginParams {
	o.SetName(name)
	return o
}

// SetName adds the name to the delete admission plugin params
func (o *DeleteAdmissionPluginParams) SetName(name string) {
	o.Name = name
}

// WriteToRequest writes these params to a swagger request
func (o *DeleteAdmissionPluginParams) WriteToRequest(r runtime.ClientRequest, reg strfmt.Registry) error {

	if err := r.SetTimeout(o.timeout); err != nil {
		return err
	}
	var res []error

	// path param name
	if err := r.SetPathParam("name", o.Name); err != nil {
		return err
	}

	if len(res) > 0 {
		return errors.CompositeValidationError(res...)
	}
	return nil
}
