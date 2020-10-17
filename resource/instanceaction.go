package resource

import (
	"github.com/jt0/gomer/gomerr"
	"github.com/jt0/gomer/gomerr/constraint"
	"github.com/jt0/gomer/limit"
	"github.com/jt0/gomer/util"
)

type InstanceAction interface {
	Pre(Instance) gomerr.Gomerr
	Do(Instance) gomerr.Gomerr
	OnDoSuccess(Instance) gomerr.Gomerr
	OnDoFailure(Instance, gomerr.Gomerr) gomerr.Gomerr
}

func DoInstanceAction(i Instance, instanceAction InstanceAction) (interface{}, gomerr.Gomerr) {
	if ge := instanceAction.Pre(i); ge != nil {
		return nil, ge
	}

	if ge := instanceAction.Do(i); ge != nil {
		return nil, instanceAction.OnDoFailure(i, ge)
	}

	if ge := instanceAction.OnDoSuccess(i); ge != nil {
		return nil, ge
	}

	return renderInstance(i)
}

type Creatable interface {
	PreCreate() gomerr.Gomerr
	PostCreate() gomerr.Gomerr
}

type OnCreateFailer interface {
	OnCreateFailure(gomerr.Gomerr) gomerr.Gomerr
}

func CreateInstance() InstanceAction {
	return &createAction{}
}

type createAction struct {
	limiter limit.Limiter
}

func (*createAction) Pre(i Instance) gomerr.Gomerr {
	if ge := i.metadata().fields.removeNonWritable(i, createAccess); ge != nil {
		return ge
	}

	if ge := i.metadata().fields.applyDefaults(i); ge != nil {
		return ge
	}

	//if err := validate.Struct(i); err != nil {
	//	return nil, gomerr.ValidationFailure(err)
	//}

	creatable, ok := i.(Creatable)
	if !ok {
		return gomerr.Unprocessable("Instance", i, constraint.TypeOf(creatable))
	}

	return creatable.PreCreate()
}

func (a *createAction) Do(i Instance) (ge gomerr.Gomerr) {
	a.limiter, ge = applyLimitAction(checkAndIncrement, i)
	if ge != nil {
		return ge
	}

	return i.metadata().dataStore.Create(i)
}

func (a *createAction) OnDoSuccess(i Instance) gomerr.Gomerr {
	defer saveLimiterIfDirty(a.limiter)

	// If we made it this far, we know i is a Creatable
	return i.(Creatable).PostCreate()
}

func (*createAction) OnDoFailure(i Instance, ge gomerr.Gomerr) gomerr.Gomerr {
	if failer, ok := i.(OnCreateFailer); ok {
		return failer.OnCreateFailure(ge)
	}

	return ge
}

type Readable interface {
	PreRead() gomerr.Gomerr
	PostRead() gomerr.Gomerr
}

type OnReadFailer interface {
	OnReadFailure(gomerr2 gomerr.Gomerr) gomerr.Gomerr
}

func ReadInstance() InstanceAction {
	return readActionSingleton
}

var readActionSingleton = &readAction{}

type readAction struct{}

func (a readAction) Pre(i Instance) gomerr.Gomerr {
	readable, ok := i.(Readable)
	if !ok {
		return gomerr.Unprocessable("Instance", i, constraint.TypeOf(readable))
	}

	return readable.PreRead()
}

func (a readAction) Do(i Instance) (ge gomerr.Gomerr) {
	return i.metadata().dataStore.Read(i)
}

func (a readAction) OnDoSuccess(i Instance) gomerr.Gomerr {
	// If we made it this far, we know i is a Readable
	return i.(Readable).PostRead()
}

func (a readAction) OnDoFailure(i Instance, ge gomerr.Gomerr) gomerr.Gomerr {
	if failer, ok := i.(OnReadFailer); ok {
		return failer.OnReadFailure(ge)
	}

	return ge
}

type Updatable interface {
	Instance
	PreUpdate(updateInstance Instance) gomerr.Gomerr
	PostUpdate(updateInstance Instance) gomerr.Gomerr
}

type OnUpdateFailer interface {
	OnUpdateFailure(gomerr2 gomerr.Gomerr) gomerr.Gomerr
}

func UpdateInstance() InstanceAction {
	return &updateAction{}
}

type updateAction struct {
	actual Updatable
}

func (a *updateAction) Pre(ui Instance) gomerr.Gomerr {
	// Clear out the update's non-writable fields (will keep 'provided' fields)
	if ge := ui.metadata().fields.removeNonWritable(ui, updateAccess); ge != nil {
		return ge
	}

	i, ge := NewInstance(util.UnqualifiedTypeName(ui), ui.Subject())
	if ge != nil {
		return ge
	}

	// Copy the 'provided' fields to the new instance
	if ge := i.metadata().fields.copyProvided(ui, i); ge != nil {
		return ge
	}

	// Populate other fields with data from the underlying store
	if ge := i.metadata().dataStore.Read(i); ge != nil {
		return ge
	}

	actual, ok := i.(Updatable)
	if !ok {
		return gomerr.Unprocessable("Instance", ui, constraint.TypeOf(actual))
	}

	a.actual = actual

	return actual.PreUpdate(ui)
}

func (a *updateAction) Do(ui Instance) (ge gomerr.Gomerr) {
	return ui.metadata().dataStore.Update(a.actual, ui)
}

func (a *updateAction) OnDoSuccess(ui Instance) gomerr.Gomerr {
	return a.actual.PostUpdate(ui)
}

func (a *updateAction) OnDoFailure(_ Instance, ge gomerr.Gomerr) gomerr.Gomerr {
	if failer, ok := a.actual.(OnUpdateFailer); ok {
		return failer.OnUpdateFailure(ge)
	}

	return ge
}

type Deletable interface {
	PreDelete() gomerr.Gomerr
	PostDelete() gomerr.Gomerr
}

type OnDeleteFailer interface {
	OnDeleteFailure(gomerr2 gomerr.Gomerr) gomerr.Gomerr
}

func DeleteInstance() InstanceAction {
	return &deleteAction{}
}

type deleteAction struct {
	limiter limit.Limiter
}

func (*deleteAction) Pre(i Instance) gomerr.Gomerr {
	deletable, ok := i.(Deletable)
	if !ok {
		return gomerr.Unprocessable("Instance", i, constraint.TypeOf(deletable))
	}

	return deletable.PreDelete()
}

func (a *deleteAction) Do(i Instance) (ge gomerr.Gomerr) {
	a.limiter, ge = applyLimitAction(decrement, i)
	if ge != nil {
		return ge
	}

	return i.metadata().dataStore.Delete(i)
}

func (a *deleteAction) OnDoSuccess(i Instance) gomerr.Gomerr {
	defer saveLimiterIfDirty(a.limiter)

	// If we made it this far, we know i is a Deletable
	return i.(Deletable).PostDelete()
}

func (*deleteAction) OnDoFailure(i Instance, ge gomerr.Gomerr) gomerr.Gomerr {
	if failer, ok := i.(OnDeleteFailer); ok {
		return failer.OnDeleteFailure(ge)
	}

	return ge
}

type Renderer interface {
	Render() (interface{}, gomerr.Gomerr)
}

type NoContentRenderer struct{}

func (NoContentRenderer) Render() (interface{}, gomerr.Gomerr) {
	return nil, nil
}

func renderInstance(i Instance) (interface{}, gomerr.Gomerr) {
	if r, ok := i.(Renderer); ok {
		return r.Render()
	}

	if result := i.metadata().fields.removeNonReadable(i); result == nil || len(result) == 0 {
		return nil, gomerr.NotFound(i.metadata().instanceName, i.Id())
	} else {
		return result, nil
	}
}

type NoOpInstanceAction struct{}

func (NoOpInstanceAction) Pre(Instance) gomerr.Gomerr {
	return nil
}

func (NoOpInstanceAction) Do(Instance) gomerr.Gomerr {
	return nil
}

func (NoOpInstanceAction) OnDoSuccess(Instance) gomerr.Gomerr {
	return nil
}

func (NoOpInstanceAction) OnDoFailure(_ Instance, ge gomerr.Gomerr) gomerr.Gomerr {
	return ge
}
