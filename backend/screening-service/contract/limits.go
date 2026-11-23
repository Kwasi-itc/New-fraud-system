// Package contract holds the provider boundary's shared resource limits.
package contract

// MaxSelectedDatasets covers the complete supported tenant catalog. Result,
// candidate and deadline bounds apply independently of the selected list count.
const MaxSelectedDatasets = 500

// MaxReadRequestBytes accommodates an explicit selection of the full catalog.
const MaxReadRequestBytes = 128 * 1024
