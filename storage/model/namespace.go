package model

import (
	"bytes"
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/rjeczalik/bigstruct/internal/types"
)

type Namespace struct {
	Model    `yaml:",inline"`
	Name     string `gorm:"column:name;type:tinytext;not null" yaml:"name,omitempty" json:"name,omitempty"`
	Property string `gorm:"-" yaml:"property,omitempty" json:"property,omitempty"`
	Priority int    `gorm:"column:priority;type:smallint;not null" yaml:"priority,omitempty" json:"priority,omitempty"`
	Metadata Object `gorm:"column:metadata;type:text" yaml:"metadata,omityempty" json:"metadata,omitempty"`
}

func (*Namespace) TableName() string {
	return TablePrefix + "_namespace"
}

func (n *Namespace) Ref() string {
	if n.Property != "" {
		return n.Name + "=" + n.Property
	}
	return n.Name
}

func (n *Namespace) Meta() *NamespaceMeta {
	var nm NamespaceMeta

	if err := n.Metadata.Unmarshal(&nm); err != nil {
		panic("unexpected error: " + err.Error())
	}

	return &nm
}

func (n *Namespace) SetProperty(prop string) error {
	switch p := n.Meta().Property; {
	case !p && prop != "":
		return fmt.Errorf("property %q not supported for %q namespace", prop, n.Name)
	case p && prop == "":
		return fmt.Errorf("property required for %q namespace", n.Name)
	default:
		n.Property = prop
	}

	return nil
}

func (n *Namespace) UpdateMeta(nm *NamespaceMeta) {
	n.Metadata = n.Meta().Update(nm).Metadata()
}

type Namespaces []*Namespace

func (ns Namespaces) WriteTab(w io.Writer) (int64, error) {
	var n int64

	m, err := fmt.Fprint(w, "ID\tNAME\tPRIORITY\tMETADATA\n")
	if err != nil {
		return int64(m), err
	}

	n += int64(m)

	for _, ns := range ns {
		m, err = fmt.Fprintf(w, "%d\t%s\t%d\t%s\n",
			ns.ID,
			ns.Name,
			ns.Priority,
			nonempty(ns.Metadata.String(), "-"),
		)

		n += int64(m)

		if err != nil {
			return n, err
		}
	}

	return n, nil
}

func (ns Namespaces) WriteTo(w io.Writer) (int64, error) {
	tw := tabwriter.NewWriter(w, 2, 0, 2, ' ', 0)

	n, err := ns.WriteTab(tw)
	if err != nil {
		return n, err
	}

	if err := tw.Flush(); err != nil {
		return n, err
	}

	return n, err

}

func (ns Namespaces) String() string {
	var buf bytes.Buffer

	if _, err := ns.WriteTo(&buf); err != nil {
		panic("unexpected error: " + err.Error())
	}

	return buf.String()
}

type NamespaceMeta struct {
	Property    bool   `json:"property"`
	CustomCodec string `json:"custom_codec,omitempty"`
}

func (nm *NamespaceMeta) Update(um *NamespaceMeta) *NamespaceMeta {
	if um == nil {
		return nm
	}

	if um.CustomCodec != "" {
		nm.CustomCodec = um.CustomCodec
	}

	return nm
}

func (nm *NamespaceMeta) JSON() types.JSON {
	return types.MakeJSON(nm)
}

func (nm *NamespaceMeta) Metadata() Object {
	return Object(nm.JSON())
}
