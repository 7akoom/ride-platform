package grpc

import (
	"context"
	"errors"

	notificationv1 "github.com/7akoom/ride-platform/gen/go/ride/notification/v1"
	"github.com/7akoom/ride-platform/services/notification-service/internal/application/notification"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type NotificationHandler struct {
	notificationv1.UnimplementedNotificationServiceServer

	notificationService notification.Service
}

func NewNotificationHandler(notificationService notification.Service) *NotificationHandler {
	if notificationService == nil {
		panic("notification service is required")
	}

	return &NotificationHandler{notificationService: notificationService}
}

func (h *NotificationHandler) Send(
	ctx context.Context,
	request *notificationv1.SendRequest,
) (*notificationv1.SendResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	channels := make([]notification.Channel, 0, len(request.GetChannels()))

	for _, channel := range request.GetChannels() {
		channels = append(channels, toDomainChannel(channel))
	}

	result, err := h.notificationService.Send(ctx, notification.SendInput{
		RecipientType:  toDomainRecipientType(request.GetRecipientType()),
		RecipientID:    request.GetRecipientId(),
		EventKey:       request.GetEventKey(),
		Variables:      request.GetVariables(),
		Data:           request.GetData(),
		Locale:         request.GetLocale(),
		Channels:       channels,
		IdempotencyKey: request.GetIdempotencyKey(),
	})
	if err != nil {
		return nil, mapNotificationError(err)
	}

	return &notificationv1.SendResponse{
		Notification: toProtoNotification(result.Notification),
		Deliveries:   toProtoDeliveries(result.Deliveries),
	}, nil
}

func (h *NotificationHandler) RegisterDevice(
	ctx context.Context,
	request *notificationv1.RegisterDeviceRequest,
) (*notificationv1.RegisterDeviceResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	deviceID, err := h.notificationService.RegisterDevice(ctx, notification.RegisterDeviceInput{
		RecipientType: toDomainRecipientType(request.GetRecipientType()),
		RecipientID:   request.GetRecipientId(),
		DeviceToken:   request.GetDeviceToken(),
		Platform:      toDomainPlatform(request.GetPlatform()),
		Locale:        request.GetLocale(),
	})
	if err != nil {
		return nil, mapNotificationError(err)
	}

	return &notificationv1.RegisterDeviceResponse{DeviceId: deviceID}, nil
}

func (h *NotificationHandler) UnregisterDevice(
	ctx context.Context,
	request *notificationv1.UnregisterDeviceRequest,
) (*notificationv1.UnregisterDeviceResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	removed, err := h.notificationService.UnregisterDevice(ctx, request.GetDeviceToken())
	if err != nil {
		return nil, mapNotificationError(err)
	}

	return &notificationv1.UnregisterDeviceResponse{Removed: removed}, nil
}

func (h *NotificationHandler) ListNotifications(
	ctx context.Context,
	request *notificationv1.ListNotificationsRequest,
) (*notificationv1.ListNotificationsResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	result, err := h.notificationService.List(ctx, notification.ListInput{
		RecipientType: toDomainRecipientType(request.GetRecipientType()),
		RecipientID:   request.GetRecipientId(),
		Limit:         int(request.GetLimit()),
		UnreadOnly:    request.GetUnreadOnly(),
	})
	if err != nil {
		return nil, mapNotificationError(err)
	}

	protoNotifications := make([]*notificationv1.Notification, len(result.Notifications))

	for i, found := range result.Notifications {
		protoNotifications[i] = toProtoNotification(found)
	}

	return &notificationv1.ListNotificationsResponse{
		Notifications: protoNotifications,
		UnreadCount:   int32(result.UnreadCount),
	}, nil
}

func (h *NotificationHandler) MarkAsRead(
	ctx context.Context,
	request *notificationv1.MarkAsReadRequest,
) (*notificationv1.MarkAsReadResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	marked, err := h.notificationService.MarkAsRead(
		ctx,
		toDomainRecipientType(request.GetRecipientType()),
		request.GetRecipientId(),
		request.GetNotificationIds(),
	)
	if err != nil {
		return nil, mapNotificationError(err)
	}

	return &notificationv1.MarkAsReadResponse{MarkedCount: int32(marked)}, nil
}

func (h *NotificationHandler) UpsertTemplate(
	ctx context.Context,
	request *notificationv1.UpsertTemplateRequest,
) (*notificationv1.UpsertTemplateResponse, error) {
	if request == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	channels := make([]notification.Channel, 0, len(request.GetDefaultChannels()))

	for _, channel := range request.GetDefaultChannels() {
		channels = append(channels, toDomainChannel(channel))
	}

	translations := make([]notification.TemplateTranslation, len(request.GetTranslations()))

	for i, translation := range request.GetTranslations() {
		translations[i] = notification.TemplateTranslation{
			EventKey: request.GetEventKey(),
			Locale:   translation.GetLocale(),
			Title:    translation.GetTitle(),
			Body:     translation.GetBody(),
		}
	}

	count, err := h.notificationService.UpsertTemplate(ctx, notification.UpsertTemplateInput{
		EventKey:        request.GetEventKey(),
		DefaultChannels: channels,
		Translations:    translations,
	})
	if err != nil {
		return nil, mapNotificationError(err)
	}

	return &notificationv1.UpsertTemplateResponse{
		EventKey:         request.GetEventKey(),
		TranslationCount: int32(count),
	}, nil
}

func mapNotificationError(err error) error {
	switch {
	case errors.Is(err, notification.ErrTemplateNotFound),
		errors.Is(err, notification.ErrTranslationNotFound):
		return status.Error(codes.NotFound, err.Error())

	case errors.Is(err, notification.ErrRecipientIDRequired),
		errors.Is(err, notification.ErrInvalidRecipientType),
		errors.Is(err, notification.ErrEventKeyRequired),
		errors.Is(err, notification.ErrDeviceTokenRequired),
		errors.Is(err, notification.ErrInvalidPlatform),
		errors.Is(err, notification.ErrInvalidChannel),
		errors.Is(err, notification.ErrTranslationsRequired):
		return status.Error(codes.InvalidArgument, err.Error())

	default:
		return status.Error(codes.Internal, "failed to process notification request")
	}
}

func toDomainRecipientType(r notificationv1.RecipientType) notification.RecipientType {
	switch r {
	case notificationv1.RecipientType_RECIPIENT_TYPE_RIDER:
		return notification.RecipientRider
	case notificationv1.RecipientType_RECIPIENT_TYPE_DRIVER:
		return notification.RecipientDriver
	default:
		return ""
	}
}

func toProtoRecipientType(r notification.RecipientType) notificationv1.RecipientType {
	switch r {
	case notification.RecipientRider:
		return notificationv1.RecipientType_RECIPIENT_TYPE_RIDER
	case notification.RecipientDriver:
		return notificationv1.RecipientType_RECIPIENT_TYPE_DRIVER
	default:
		return notificationv1.RecipientType_RECIPIENT_TYPE_UNSPECIFIED
	}
}

func toDomainChannel(c notificationv1.Channel) notification.Channel {
	switch c {
	case notificationv1.Channel_CHANNEL_IN_APP:
		return notification.ChannelInApp
	case notificationv1.Channel_CHANNEL_PUSH:
		return notification.ChannelPush
	case notificationv1.Channel_CHANNEL_SMS:
		return notification.ChannelSMS
	default:
		return ""
	}
}

func toProtoChannel(c notification.Channel) notificationv1.Channel {
	switch c {
	case notification.ChannelInApp:
		return notificationv1.Channel_CHANNEL_IN_APP
	case notification.ChannelPush:
		return notificationv1.Channel_CHANNEL_PUSH
	case notification.ChannelSMS:
		return notificationv1.Channel_CHANNEL_SMS
	default:
		return notificationv1.Channel_CHANNEL_UNSPECIFIED
	}
}

func toDomainPlatform(p notificationv1.Platform) notification.Platform {
	switch p {
	case notificationv1.Platform_PLATFORM_ANDROID:
		return notification.PlatformAndroid
	case notificationv1.Platform_PLATFORM_IOS:
		return notification.PlatformIOS
	case notificationv1.Platform_PLATFORM_WEB:
		return notification.PlatformWeb
	default:
		return ""
	}
}

func toProtoStatus(s notification.DeliveryStatus) notificationv1.DeliveryStatus {
	switch s {
	case notification.StatusPending:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_PENDING
	case notification.StatusSent:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_SENT
	case notification.StatusFailed:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_FAILED
	case notification.StatusSkipped:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_SKIPPED
	default:
		return notificationv1.DeliveryStatus_DELIVERY_STATUS_UNSPECIFIED
	}
}

func toProtoNotification(n notification.Notification) *notificationv1.Notification {
	return &notificationv1.Notification{
		Id:            n.ID,
		RecipientType: toProtoRecipientType(n.RecipientType),
		RecipientId:   n.RecipientID,
		EventKey:      n.EventKey,
		Title:         n.Title,
		Body:          n.Body,
		Locale:        n.Locale,
		Data:          n.Data,
		Read:          n.Read,
		CreatedAt:     timestamppb.New(n.CreatedAt),
	}
}

func toProtoDeliveries(deliveries []notification.Delivery) []*notificationv1.Delivery {
	protoDeliveries := make([]*notificationv1.Delivery, len(deliveries))

	for i, delivery := range deliveries {
		protoDeliveries[i] = &notificationv1.Delivery{
			Channel: toProtoChannel(delivery.Channel),
			Status:  toProtoStatus(delivery.Status),
			Detail:  delivery.Detail,
		}
	}

	return protoDeliveries
}
