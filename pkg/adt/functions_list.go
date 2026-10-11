package adt

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/url"
	"strings"
)

// A function group's metadata document does not carry its modules. It never has,
// on any release measured — so GetFunctionGroup returned a group whose Functions
// were always nil, while the tool it backs is described as returning the
// function module list. Callers asking "what is in this group" got an empty
// answer that looked like an empty group.
//
// The modules hang off the repository node structure, which is what Eclipse's
// object tree reads. Two other endpoints look like candidates and are not:
//
//   - objectstructure exists on S/4-generation systems and answers 404 on
//     ERP-generation ones, which do not advertise the relation at all.
//   - the same node structure answers 406 to every vendor content type tried on
//     S/4, and 200 to all of them on ERP. The one Accept value both accept is
//     */*, which is therefore not laziness but the portable choice.
//
// Measured on 7.57/HANA and on 7.50/MSSQL; the response shape is identical.

// repositoryNode is one entry of the repository node structure.
type repositoryNode struct {
	ObjectType string `xml:"OBJECT_TYPE"`
	ObjectName string `xml:"OBJECT_NAME"`
	TechName   string `xml:"TECH_NAME"`
	ObjectURI  string `xml:"OBJECT_URI"`
}

// repositoryNodeStructure is the document the node structure endpoint returns:
// an ABAP XML envelope, not an ADT resource, with the nodes under
// asx:values/DATA/TREE_CONTENT.
type repositoryNodeStructure struct {
	XMLName xml.Name         `xml:"abap"`
	Nodes   []repositoryNode `xml:"values>DATA>TREE_CONTENT>SEU_ADT_REPOSITORY_OBJ_NODE"`
}

// ListFunctionModules returns the modules of a function group.
func (c *Client) ListFunctionModules(ctx context.Context, groupName string) ([]FunctionModule, error) {
	groupName = strings.ToUpper(strings.TrimSpace(groupName))
	if groupName == "" {
		return nil, fmt.Errorf("function group name is required")
	}
	nodes, err := c.functionGroupNodes(ctx, groupName)
	if err != nil {
		return nil, err
	}
	return functionModulesFromNodes(nodes), nil
}

// functionGroupNodes reads the node structure of a function group (name in
// upper case): its includes and modules, on both releases.
func (c *Client) functionGroupNodes(ctx context.Context, groupName string) ([]repositoryNode, error) {
	query := url.Values{}
	query.Set("parent_type", "FUGR/F")
	query.Set("parent_name", groupName)
	query.Set("withShortDescriptions", "true")

	resp, err := c.transport.Request(ctx, "/sap/bc/adt/repository/nodestructure", &RequestOptions{
		Method: http.MethodPost,
		Query:  query,
		// See the note above: every vendor content type is refused on one
		// release or the other.
		Accept: "*/*",
	})
	if err != nil {
		return nil, fmt.Errorf("listing modules of function group %s: %w", groupName, err)
	}

	var doc repositoryNodeStructure
	if err := xml.Unmarshal(resp.Body, &doc); err != nil {
		return nil, fmt.Errorf("parsing the node structure of function group %s: %w", groupName, err)
	}

	return doc.Nodes, nil
}

// functionModulesFromNodes keeps the modules and drops everything else.
//
// Two kinds of entry have to be filtered out. A group's includes are listed
// beside its modules — LxxxTOP, LxxxUXX and friends — and are separated by
// object type. Less obviously, the structure also carries a header row per
// category: an entry with a type and nothing else, since it stands for the
// folder rather than for an object. Its TECH_NAME holds the group's main
// program, so falling back to that field turns each header into a module named
// SAPL<group> that does not exist. A real module has a name of its own.
func functionModulesFromNodes(nodes []repositoryNode) []FunctionModule {
	var modules []FunctionModule
	for _, n := range nodes {
		if !strings.EqualFold(strings.TrimSpace(n.ObjectType), "FUGR/FF") {
			continue
		}
		name := strings.TrimSpace(n.ObjectName)
		if name == "" {
			continue
		}
		modules = append(modules, FunctionModule{
			Name: name,
			Type: "FUGR/FF",
			URI:  strings.TrimSpace(n.ObjectURI),
		})
	}
	return modules
}
