package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"text/tabwriter"
)

// selectOrganization picks the organization for subsequent commands: the already
// configured one when the principal still belongs to it, else the first.
func selectOrganization(cfg *Config, organizations []Organization) Organization {
	selected := organizations[0]
	for _, t := range organizations {
		if t.ID == cfg.LocalConfig.OrganizationID {
			selected = t
			break
		}
	}
	cfg.LocalConfig.OrganizationID = selected.ID
	cfg.LocalConfig.OrganizationName = selected.Name
	return selected
}

// organization is one the login belongs to
type organizationEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Current bool   `json:"current"` // the one flop's commands use
}

type organizationList struct {
	Organizations []organizationEntry `json:"organizations"`
}

func (l organizationList) human(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tNAME\t")
	for _, t := range l.Organizations {
		current := ""
		if t.Current {
			current = "(current)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\n", t.ID, t.Name, current)
	}
	tw.Flush()
}

func ListOrganizations(ctx context.Context) (organizationList, error) {
	cfg, err := LoadConfig()
	if err != nil {
		return organizationList{}, err
	}
	if cfg.LocalConfig.APIKey == "" {
		return organizationList{}, errNotLoggedIn
	}
	organizations, err := NewClient(cfg).ListOrganizations(ctx)
	if err != nil {
		return organizationList{}, fmt.Errorf("list organizations: %w", err)
	}
	out := organizationList{Organizations: []organizationEntry{}}
	for _, t := range organizations {
		out.Organizations = append(out.Organizations, organizationEntry{ID: t.ID, Name: t.Name, Current: t.ID == cfg.LocalConfig.OrganizationID})
	}
	return out, nil
}

// organizationUsed is the organization flop's commands now use
type organizationUsed struct {
	Organization organizationEntry `json:"organization"`
}

func (u organizationUsed) human(w io.Writer) {
	fmt.Fprintf(w, "Using organization: %s (%s)\n", u.Organization.Name, u.Organization.ID)
}

func UseOrganization(ctx context.Context, idOrName string) (organizationUsed, error) {
	if idOrName == "" {
		return organizationUsed{}, usage("organization use needs an organization", "flop organization use <id or name> (see `flop organization list`)")
	}
	cfg, err := LoadConfig()
	if err != nil {
		return organizationUsed{}, err
	}
	if cfg.LocalConfig.APIKey == "" {
		return organizationUsed{}, errNotLoggedIn
	}
	if os.Getenv("FLOP_TOKEN") != "" {
		return organizationUsed{}, usage("an organization token belongs to one organization", "set FLOP_ORGANIZATION_ID instead")
	}

	organizations, err := NewClient(cfg).ListOrganizations(ctx)
	if err != nil {
		return organizationUsed{}, fmt.Errorf("list organizations: %w", err)
	}
	var match *Organization
	for i, t := range organizations {
		if t.ID == idOrName {
			match = &organizations[i]
			break
		}
		if t.Name == idOrName {
			if match != nil {
				return organizationUsed{}, usage(fmt.Sprintf("organization name %q is ambiguous", idOrName), "use its id (`flop organization list`)")
			}
			match = &organizations[i]
		}
	}
	if match == nil {
		return organizationUsed{}, fail("not_found", fmt.Sprintf("no organization %q", idOrName), "see `flop organization list`")
	}

	cfg.LocalConfig.OrganizationID = match.ID
	cfg.LocalConfig.OrganizationName = match.Name
	if err := SaveLocalConfig(cfg.LocalConfig); err != nil {
		return organizationUsed{}, fmt.Errorf("save local config: %w", err)
	}
	return organizationUsed{Organization: organizationEntry{ID: match.ID, Name: match.Name, Current: true}}, nil
}
