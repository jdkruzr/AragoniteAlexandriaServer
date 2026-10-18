package web

import (
	"strings"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/caldav"
	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/taskhost"
)

type calDAVConnection struct {
	Discovery, Tasks, Calendar string
	Kind                       string
}

func connection(base, kind string) calDAVConnection {
	base = strings.TrimRight(base, "/")
	return calDAVConnection{base + taskhost.Prefix + "/", base + taskhost.Prefix + caldav.TasksCollection, base + taskhost.Prefix + caldav.EventsCollection, kind}
}
