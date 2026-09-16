// Package app composes Dockhand's configuration, workflow engine, and concrete
// integrations for command callers.
//
// Build opens repository-scoped state and constructs services whose lifetime ends
// with Services.Close. Separate entry points support setup, authentication,
// previews, discovery, status, and maintenance with the dependencies each needs.
// Provider selection and request binding happen here; accepted-job progression
// and bookkeeping belong to workflow, and presentation belongs to cli.
package app
