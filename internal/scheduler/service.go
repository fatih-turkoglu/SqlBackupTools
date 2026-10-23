package scheduler

// Service, Görev Zamanlayıcı işlemleridir. CLI ve arayüz bunu kullanır;
// testlerde sahtesi verilir.
type Service interface {
	Create(name, taskXML, runAs string) error
	Delete(name string) error
	Run(name string) error
	Query(name string) (TaskStatus, error)
}

// System, gerçek Görev Zamanlayıcı'dır.
type System struct{}

func (System) Create(name, x, runAs string) error    { return CreateTask(name, x, runAs) }
func (System) Delete(name string) error              { return DeleteTask(name) }
func (System) Run(name string) error                 { return RunTask(name) }
func (System) Query(name string) (TaskStatus, error) { return QueryTask(name) }
