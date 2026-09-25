package integrations

import "github.com/kubewhy/kubewhy/internal/model"

type Notifier interface {
	Notify(report model.Report) error
}
