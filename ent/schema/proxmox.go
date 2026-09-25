package schema

import (
	"entgo.io/ent"
	"entgo.io/ent/schema/field"
)

type Proxmox struct {
	ent.Schema
}

func (Proxmox) Fields() []ent.Field {
	return []ent.Field{
		field.String("token").Default("").Comment("Proxmox API token (user@pam!tokenid=uuid)"),
		field.String("url").Default("https://proxmox:8006/api2/json/nodes").Comment("Proxmox nodes API URL"),
	}
}
