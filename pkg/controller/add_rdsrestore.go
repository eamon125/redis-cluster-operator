package controller

import (
	"github.com/dongxiaoyi/rds-operator/pkg/controller/rdsrestore"
)

func init() {
	// AddToManagerFuncs is a list of functions to create controllers and add them to a manager.
	AddToManagerFuncs = append(AddToManagerFuncs, rdsrestore.Add)
}
