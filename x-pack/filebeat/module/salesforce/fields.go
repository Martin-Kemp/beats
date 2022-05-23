// Copyright Elasticsearch B.V. and/or licensed to Elasticsearch B.V. under one
// or more contributor license agreements. Licensed under the Elastic License;
// you may not use this file except in compliance with the Elastic License.

// Code generated by beats/dev-tools/cmd/asset/asset.go - DO NOT EDIT.

package salesforce

import (
	"github.com/elastic/beats/v7/libbeat/asset"
)

func init() {
	if err := asset.SetFields("filebeat", "salesforce", asset.ModuleFieldsPri, AssetSalesforce); err != nil {
		panic(err)
	}
}

// AssetSalesforce returns asset data.
// This is the base64 encoded zlib format compressed contents of module/salesforce.
func AssetSalesforce() string {
	return "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"
}