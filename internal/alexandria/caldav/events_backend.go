package caldav

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	ical "github.com/emersion/go-ical"
	"github.com/emersion/go-webdav"
	gocaldav "github.com/emersion/go-webdav/caldav"

	"github.com/jdkruzr/AragoniteAlexandriaServer/internal/alexandria/events"
)

// EventsCollection is the events calendar's path below the CalDAV prefix.
const EventsCollection = "/user/calendars/events/"

// EventStore is what the events collection needs. *events.Store satisfies it.
type EventStore interface {
	List(ctx context.Context) ([]events.Event, error)
	Get(ctx context.Context, id string) (events.Event, error)
	Create(ctx context.Context, e *events.Event) error
	Update(ctx context.Context, e *events.Event) error
	Delete(ctx context.Context, id string) error
	MaxUpdatedAtAll(ctx context.Context) (int64, error)
	ListChangedSince(ctx context.Context, sinceMs int64) ([]events.Event, error)
	SyncFloor(ctx context.Context) (int64, error)
}

// EventBackend serves the events collection (VEVENT only). Discovery lists
// both collections; requests below EventsCollection are routed here.
type EventBackend struct {
	store  EventStore
	prefix string
	// zone reads floating times and TZIDs Go doesn't know.
	zone *time.Location
	// tasks is the Tasks collection, listed beside this one.
	tasks func() gocaldav.Calendar

	nameMu sync.RWMutex
	name   string
}

func NewEventBackend(store EventStore, prefix, name string, zone *time.Location) *EventBackend {
	if zone == nil {
		zone = time.UTC
	}
	return &EventBackend{store: store, prefix: strings.TrimSuffix(prefix, "/"), name: name, zone: zone}
}

func (b *EventBackend) SetCollectionName(name string) {
	b.nameMu.Lock()
	defer b.nameMu.Unlock()
	b.name = name
}

// Collection describes the events calendar.
func (b *EventBackend) Collection() gocaldav.Calendar {
	b.nameMu.RLock()
	defer b.nameMu.RUnlock()
	return gocaldav.Calendar{
		Path:                  b.prefix + EventsCollection,
		Name:                  b.name,
		Description:           "Events in Alexandria",
		SupportedComponentSet: []string{"VEVENT"},
	}
}

func (b *EventBackend) CurrentUserPrincipal(context.Context) (string, error) {
	return b.prefix + "/user/", nil
}

func (b *EventBackend) CalendarHomeSetPath(context.Context) (string, error) {
	return b.prefix + "/user/calendars/", nil
}

func (b *EventBackend) CreateCalendar(context.Context, *gocaldav.Calendar) error {
	return fmt.Errorf("calendar creation not supported")
}

func (b *EventBackend) ListCalendars(context.Context) ([]gocaldav.Calendar, error) {
	if b.tasks != nil {
		return []gocaldav.Calendar{b.tasks(), b.Collection()}, nil
	}
	return []gocaldav.Calendar{b.Collection()}, nil
}

func (b *EventBackend) GetCalendar(_ context.Context, urlPath string) (*gocaldav.Calendar, error) {
	col := b.Collection()
	if path.Clean(urlPath) != path.Clean(col.Path) {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("calendar not found"))
	}
	return &col, nil
}

// ObjectPath is an event's href.
func (b *EventBackend) ObjectPath(id string) string {
	return b.prefix + EventsCollection + id + ".ics"
}

func eventIDFromPath(urlPath string) string {
	if !strings.HasSuffix(urlPath, ".ics") {
		return ""
	}
	return strings.TrimSuffix(path.Base(urlPath), ".ics")
}

// EventETag changes whenever a stored event does.
func EventETag(e events.Event) string {
	h := md5.Sum([]byte(e.ID + "\x00" + strconv.FormatInt(e.UpdatedAt, 10)))
	return hex.EncodeToString(h[:])
}

func (b *EventBackend) object(e events.Event) *gocaldav.CalendarObject {
	return &gocaldav.CalendarObject{
		Path:    b.ObjectPath(e.ID),
		ModTime: time.UnixMilli(e.UpdatedAt),
		ETag:    EventETag(e),
		Data:    EventToCalendar(e),
	}
}

func notFound(err error) error {
	if errors.Is(err, events.ErrNotFound) {
		return webdav.NewHTTPError(http.StatusNotFound, err)
	}
	return err
}

func (b *EventBackend) GetCalendarObject(ctx context.Context, urlPath string, _ *gocaldav.CalendarCompRequest) (*gocaldav.CalendarObject, error) {
	id := eventIDFromPath(urlPath)
	if id == "" {
		return nil, webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("invalid path"))
	}
	e, err := b.store.Get(ctx, id)
	if err != nil {
		return nil, notFound(err)
	}
	return b.object(e), nil
}

func (b *EventBackend) ListCalendarObjects(ctx context.Context, _ string, _ *gocaldav.CalendarCompRequest) ([]gocaldav.CalendarObject, error) {
	list, err := b.store.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gocaldav.CalendarObject, len(list))
	for i := range list {
		out[i] = *b.object(list[i])
	}
	return out, nil
}

// QueryCalendarObjects applies the full calendar-query filter (component,
// properties and time ranges) with go-webdav's matcher.
func (b *EventBackend) QueryCalendarObjects(ctx context.Context, urlPath string, query *gocaldav.CalendarQuery) ([]gocaldav.CalendarObject, error) {
	all, err := b.ListCalendarObjects(ctx, urlPath, nil)
	return filterCalendarObjects(all, query, err)
}

func (b *EventBackend) PutCalendarObject(ctx context.Context, urlPath string, cal *ical.Calendar, _ *gocaldav.PutCalendarObjectOptions) (*gocaldav.CalendarObject, error) {
	if FindVEVENT(cal) == nil || HasVTodo(cal) {
		return nil, gocaldav.NewPreconditionError(gocaldav.PreconditionSupportedCalendarComponent)
	}
	id := eventIDFromPath(urlPath)
	if id == "" {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, fmt.Errorf("invalid path"))
	}
	e, err := VEVENTToEvent(cal, b.zone)
	if err != nil {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	e.ID = id
	if existing, gerr := b.store.Get(ctx, id); gerr == nil {
		e.CreatedAt = existing.CreatedAt
		err = b.store.Update(ctx, &e)
	} else if errors.Is(gerr, events.ErrNotFound) {
		err = b.store.Create(ctx, &e)
		// A device that missed a deletion must not bring the event back.
		if errors.Is(err, events.ErrDeleted) || errors.Is(err, events.ErrPurged) {
			return nil, webdav.NewHTTPError(http.StatusPreconditionFailed, err)
		}
	} else {
		return nil, gerr
	}
	if errors.Is(err, events.ErrInvalid) {
		return nil, webdav.NewHTTPError(http.StatusBadRequest, err)
	}
	if err != nil {
		return nil, err
	}
	stored, err := b.store.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return b.object(stored), nil
}

func (b *EventBackend) DeleteCalendarObject(ctx context.Context, urlPath string) error {
	id := eventIDFromPath(urlPath)
	if id == "" {
		return webdav.NewHTTPError(http.StatusNotFound, fmt.Errorf("invalid path"))
	}
	return notFound(b.store.Delete(ctx, id))
}

// eventSync adapts the event store to the sync-collection report.
type eventSync struct {
	store   EventStore
	backend *EventBackend
}

func (s eventSync) MaxUpdatedAtAll(ctx context.Context) (int64, error) {
	return s.store.MaxUpdatedAtAll(ctx)
}
func (s eventSync) SyncFloor(ctx context.Context) (int64, error) { return s.store.SyncFloor(ctx) }

func (s eventSync) Changes(ctx context.Context, since int64, initial bool) ([]SyncItem, error) {
	var list []events.Event
	var err error
	if initial {
		list, err = s.store.List(ctx)
	} else {
		list, err = s.store.ListChangedSince(ctx, since)
	}
	if err != nil {
		return nil, err
	}
	out := make([]SyncItem, len(list))
	for i := range list {
		e := list[i]
		out[i] = SyncItem{Href: s.backend.ObjectPath(e.ID), ETag: EventETag(e), Deleted: e.Deleted, UpdatedAt: e.UpdatedAt,
			Calendar: func() *ical.Calendar { return EventToCalendar(e) }}
	}
	return out, nil
}

// EventSyncSource is the events collection's sync-collection source.
func EventSyncSource(store EventStore, backend *EventBackend) SyncSource {
	return eventSync{store: store, backend: backend}
}

// WithTasks lists the Tasks collection beside events in discovery.
func (b *EventBackend) WithTasks(tasks func() gocaldav.Calendar) { b.tasks = tasks }
