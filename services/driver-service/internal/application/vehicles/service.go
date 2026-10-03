package vehicles

import (
	"context"
	"encoding/base64"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// MaxInService is how many cars a driver may have that are not retired.
	MaxInService = 5

	minYear         = 1980
	maxReasonLength = 500
	defaultPageSize = 20
	maxPageSize     = 100
	pageTokenPrefix = "v1:"

	classEconomy = "economy"
	classComfort = "comfort"
)

var uuidShape = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

type Service struct {
	repository Repository
	documents  Documents
	media      Media
	ids        IDGenerator
	clock      Clock
	logger     *slog.Logger
}

func NewService(repository Repository, documents Documents, media Media, ids IDGenerator, clock Clock, logger *slog.Logger) *Service {
	if repository == nil || documents == nil || media == nil || ids == nil || clock == nil || logger == nil {
		panic("vehicle service dependencies are required")
	}

	return &Service{repository: repository, documents: documents, media: media, ids: ids, clock: clock, logger: logger}
}

// Details are what the driver says about a car.
type Details struct {
	Make        string
	Model       string
	Color       string
	PlateNumber string
	Year        int
	Class       string
}

func (s *Service) clean(d Details) (Details, error) {
	d.Make = strings.TrimSpace(d.Make)
	d.Model = strings.TrimSpace(d.Model)
	d.Color = strings.TrimSpace(d.Color)
	d.PlateNumber = strings.ToUpper(strings.TrimSpace(d.PlateNumber))

	switch {
	case d.Make == "", d.Model == "", d.PlateNumber == "",
		utf8.RuneCountInString(d.Make) > 60, utf8.RuneCountInString(d.Model) > 60,
		utf8.RuneCountInString(d.Color) > 40, utf8.RuneCountInString(d.PlateNumber) > 20:
		return Details{}, ErrInvalidVehicle
	case d.Year == 0:
		return Details{}, ErrYearRequired
	case !s.validYear(d.Year):
		return Details{}, ErrInvalidYear
	}

	class, err := cleanClass(d.Class)
	if err != nil {
		return Details{}, err
	}

	d.Class = class

	return d, nil
}

func (s *Service) validYear(year int) bool {
	return year >= minYear && year <= s.clock.Now().Year()+1
}

func cleanClass(raw string) (string, error) {
	switch class := strings.ToLower(strings.TrimSpace(raw)); class {
	case "":
		return classEconomy, nil
	case classEconomy, classComfort:
		return class, nil
	default:
		return "", ErrInvalidClass
	}
}

// Add registers another car; it waits for review.
func (s *Service) Add(ctx context.Context, driverID string, details Details) (Vehicle, error) {
	driverID, err := checkID(driverID, ErrDriverNotFound)
	if err != nil {
		return Vehicle{}, err
	}

	d, err := s.clean(details)
	if err != nil {
		return Vehicle{}, err
	}

	added, err := s.repository.Add(ctx, Vehicle{
		ID: s.ids.NewID(), DriverID: driverID,
		Make: d.Make, Model: d.Model, Color: d.Color, PlateNumber: d.PlateNumber, Year: d.Year, Class: d.Class,
		Status: StatusPending,
	}, MaxInService)
	if err != nil {
		return Vehicle{}, fmt.Errorf("add vehicle: %w", err)
	}

	return added, nil
}

// List returns the driver's cars.
func (s *Service) List(ctx context.Context, driverID string) ([]Vehicle, error) {
	driverID, err := checkID(driverID, ErrDriverNotFound)
	if err != nil {
		return nil, err
	}

	found, err := s.repository.ListByDriver(ctx, driverID)
	if err != nil {
		return nil, fmt.Errorf("list vehicles: %w", err)
	}

	return found, nil
}

// Update corrects a car still under review, or sends a rejected one back to it.
func (s *Service) Update(ctx context.Context, driverID, vehicleID string, details Details) (Vehicle, error) {
	current, err := s.owned(ctx, driverID, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	if current.Status != StatusPending && current.Status != StatusRejected {
		return Vehicle{}, ErrVehicleNotEditable
	}

	d, err := s.clean(details)
	if err != nil {
		return Vehicle{}, err
	}

	updated, err := s.repository.Update(ctx, Vehicle{
		ID: current.ID, DriverID: current.DriverID,
		Make: d.Make, Model: d.Model, Color: d.Color, PlateNumber: d.PlateNumber, Year: d.Year, Class: d.Class,
	})
	if err != nil {
		return Vehicle{}, fmt.Errorf("update vehicle: %w", err)
	}

	return updated, nil
}

// Activate makes an approved car the one the driver works with.
func (s *Service) Activate(ctx context.Context, driverID, vehicleID string) (Vehicle, error) {
	current, err := s.owned(ctx, driverID, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	activated, err := s.repository.Activate(ctx, current.DriverID, current.ID)
	if err != nil {
		return Vehicle{}, fmt.Errorf("activate vehicle: %w", err)
	}

	return activated, nil
}

// Retire takes a car out of service; its document files are deleted.
func (s *Service) Retire(ctx context.Context, driverID, vehicleID string) (Vehicle, error) {
	current, err := s.owned(ctx, driverID, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	retired, files, err := s.repository.Retire(ctx, current.DriverID, current.ID)
	if err != nil {
		return Vehicle{}, fmt.Errorf("retire vehicle: %w", err)
	}

	for _, file := range files {
		if err := s.media.Discard(ctx, file); err != nil {
			s.logger.Warn("failed to delete a retired car's document file", "media_id", file, "error", err)
		}
	}

	return retired, nil
}

type ApproveInput struct {
	VehicleID  string
	Year       int
	Class      string
	ReviewedBy string
}

// Approve approves a pending car once its required documents are approved.
func (s *Service) Approve(ctx context.Context, in ApproveInput) (Vehicle, error) {
	current, err := s.load(ctx, in.VehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	if current.Status != StatusPending {
		return Vehicle{}, ErrVehicleNotPending
	}

	year := current.Year
	if in.Year != 0 {
		year = in.Year
	}

	switch {
	case year == 0:
		return Vehicle{}, ErrYearRequired
	case !s.validYear(year):
		return Vehicle{}, ErrInvalidYear
	}

	class := current.Class
	if strings.TrimSpace(in.Class) != "" {
		if class, err = cleanClass(in.Class); err != nil {
			return Vehicle{}, err
		}
	}

	missing, err := s.documents.VehicleDocumentsMissing(ctx, current.DriverID, current.ID)
	if err != nil {
		return Vehicle{}, fmt.Errorf("check the car's documents: %w", err)
	}

	if len(missing) > 0 {
		return Vehicle{}, fmt.Errorf("%w: %s", ErrDocumentsIncomplete, strings.Join(missing, ", "))
	}

	approved, err := s.repository.Approve(ctx, ApproveRecord{
		VehicleID: current.ID, Year: year, Class: class, ReviewedBy: reviewer(in.ReviewedBy),
	})
	if err != nil {
		return Vehicle{}, fmt.Errorf("approve vehicle: %w", err)
	}

	return approved, nil
}

type RejectInput struct {
	VehicleID  string
	Reason     string
	ReviewedBy string
}

// Reject turns down a pending car.
func (s *Service) Reject(ctx context.Context, in RejectInput) (Vehicle, error) {
	reason := strings.TrimSpace(in.Reason)

	switch {
	case reason == "":
		return Vehicle{}, ErrReasonRequired
	case utf8.RuneCountInString(reason) > maxReasonLength:
		return Vehicle{}, ErrReasonTooLong
	}

	current, err := s.load(ctx, in.VehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	if current.Status != StatusPending {
		return Vehicle{}, ErrVehicleNotPending
	}

	rejected, err := s.repository.Reject(ctx, RejectRecord{VehicleID: current.ID, Reason: reason, ReviewedBy: reviewer(in.ReviewedBy)})
	if err != nil {
		return Vehicle{}, fmt.Errorf("reject vehicle: %w", err)
	}

	return rejected, nil
}

type PendingPage struct {
	Items         []PendingItem
	NextPageToken string
}

// Pending is the car review queue, oldest first.
func (s *Service) Pending(ctx context.Context, pageSize int, pageToken string) (PendingPage, error) {
	switch {
	case pageSize < 0:
		return PendingPage{}, ErrInvalidPageSize
	case pageSize == 0:
		pageSize = defaultPageSize
	case pageSize > maxPageSize:
		pageSize = maxPageSize
	}

	query := PendingQuery{Limit: pageSize + 1}

	if pageToken != "" {
		after, id, err := decodePageToken(pageToken)
		if err != nil {
			return PendingPage{}, err
		}

		query.AfterCreatedAt, query.AfterID = after, id
	}

	items, err := s.repository.ListPending(ctx, query)
	if err != nil {
		return PendingPage{}, fmt.Errorf("list pending vehicles: %w", err)
	}

	page := PendingPage{Items: items}

	if len(items) > pageSize {
		last := items[pageSize-1].Vehicle
		page.Items = items[:pageSize]
		page.NextPageToken = base64.RawURLEncoding.EncodeToString(
			[]byte(pageTokenPrefix + last.CreatedAt.UTC().Format(time.RFC3339Nano) + "|" + last.ID))
	}

	return page, nil
}

// Get reads one car.
func (s *Service) Get(ctx context.Context, vehicleID string) (Vehicle, error) {
	return s.load(ctx, vehicleID)
}

// owned loads a car of the driver; someone else's car looks the same as none.
func (s *Service) owned(ctx context.Context, driverID, vehicleID string) (Vehicle, error) {
	driverID, err := checkID(driverID, ErrDriverNotFound)
	if err != nil {
		return Vehicle{}, err
	}

	current, err := s.load(ctx, vehicleID)
	if err != nil {
		return Vehicle{}, err
	}

	if current.DriverID != driverID {
		return Vehicle{}, ErrVehicleNotFound
	}

	return current, nil
}

func (s *Service) load(ctx context.Context, vehicleID string) (Vehicle, error) {
	id, err := checkID(vehicleID, ErrVehicleNotFound)
	if err != nil {
		return Vehicle{}, err
	}

	return s.repository.Get(ctx, id)
}

func checkID(raw string, notFound error) (string, error) {
	id := strings.ToLower(strings.TrimSpace(raw))
	if !uuidShape.MatchString(id) {
		return "", notFound
	}

	return id, nil
}

func reviewer(identityID string) string {
	id := strings.ToLower(strings.TrimSpace(identityID))
	if !uuidShape.MatchString(id) {
		return ""
	}

	return id
}

func decodePageToken(token string) (time.Time, string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return time.Time{}, "", ErrInvalidPageToken
	}

	rest, ok := strings.CutPrefix(string(raw), pageTokenPrefix)
	if !ok {
		return time.Time{}, "", ErrInvalidPageToken
	}

	at, id, ok := strings.Cut(rest, "|")
	if !ok || !uuidShape.MatchString(id) {
		return time.Time{}, "", ErrInvalidPageToken
	}

	createdAt, err := time.Parse(time.RFC3339Nano, at)
	if err != nil {
		return time.Time{}, "", ErrInvalidPageToken
	}

	return createdAt, id, nil
}
