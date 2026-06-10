package azuredevops

// AzureStatus models the Azure DevOps status payload, with the nested
// "context" structure.
type AzureStatus struct {
	State       string       `json:"state"`
	Description string       `json:"description,omitempty"`
	TargetURL   string       `json:"targetUrl,omitempty"`
	Context     AzureContext `json:"context"`
}

// AzureContext represents the context field in Azure DevOps status.
type AzureContext struct {
	Name  string `json:"name"`
	Genre string `json:"genre,omitempty"`
}
