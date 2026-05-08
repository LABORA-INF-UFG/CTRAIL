package camara

// TrafficInfluenceRequest defines the request body for creating a traffic influence subscription
// based on CAMARA Traffic Influence API.
type TrafficInfluenceRequest struct {
	AppId                     string                    `json:"appId"`                               //
	EdgeCloudRegion           string                    `json:"edgeCloudRegion,omitempty"`           //
	EdgeCloudZoneId           string                    `json:"edgeCloudZoneId,omitempty"`           //
	AppInstanceId             string                    `json:"appInstanceId,omitempty"`             //
	Device                    Device                    `json:"device,omitempty"`                    //
	SourceTrafficFilters      *SourceTrafficFilter      `json:"sourceTrafficFilters,omitempty"`      //
	DestinationTrafficFilters *DestinationTrafficFilter `json:"destinationTrafficFilters,omitempty"` //
	NotificationUri           string                    `json:"notificationUri,omitempty"`           //
	NotificationAuthToken     string                    `json:"notificationAuthToken,omitempty"`     //
	NotificationSink          *NotificationSink         `json:"notificationSink,omitempty"`          //
}

// TrafficInfluenceResponse defines the response body for traffic influence operations.
type TrafficInfluenceResponse struct {
	TrafficInfluenceID        string                    `json:"trafficInfluenceID"`
	ApiConsumerId             string                    `json:"apiConsumerId,omitempty"`
	AppId                     string                    `json:"appId,omitempty"`
	AppInstanceId             string                    `json:"appInstanceId,omitempty"`
	EdgeCloudRegion           string                    `json:"edgeCloudRegion,omitempty"`
	EdgeCloudZoneId           string                    `json:"edgeCloudZoneId,omitempty"`
	State                     string                    `json:"state"` // e.g., "ordered", "created", "active", "error"
	SourceTrafficFilters      *SourceTrafficFilter      `json:"sourceTrafficFilters,omitempty"`
	DestinationTrafficFilters *DestinationTrafficFilter `json:"destinationTrafficFilters,omitempty"`
	NotificationUri           string                    `json:"notificationUri,omitempty"`
	SelectedAppInstanceId     string                    `json:"selected_appInstanceId,omitempty"`
	// Device field is typically not returned in GET responses for privacy reasons as per CAMARA spec.
}

type Device struct {
	IPv4Address *IPv4Address `json:"ipv4Address,omitempty"` //
	PhoneNumber string       `json:"phoneNumber,omitempty"` //
}

type IPv4Address struct {
	PublicAddress  string `json:"publicAddress"`            //
	PrivateAddress string `json:"privateAddress,omitempty"` //
	PublicPort     int    `json:"publicPort,omitempty"`     //
}

type IPv6Address struct {
	PublicAddress  string `json:"publicAddress"`            //
	PrivateAddress string `json:"privateAddress,omitempty"` //
	PublicPort     int    `json:"publicPort,omitempty"`     //
	PrivatePort    int    `json:"privatePort,omitempty"`    //
}

// SourceTrafficFilter defines source port for traffic influence.
type SourceTrafficFilter struct {
	SourcePort int `json:"sourcePort,omitempty"`
}

// DestinationTrafficFilter defines destination port and protocol.
type DestinationTrafficFilter struct {
	DestinationPort     int    `json:"destinationPort,omitempty"`
	DestinationProtocol string `json:"destinationProtocol,omitempty"` // e.g., TCP, UDP
}

// NotificationSink defines the callback URI and credentials.
type NotificationSink struct {
	Sink            string           `json:"sink"`
	SinkCredentials *SinkCredentials `json:"sinkCredentials,omitempty"`
}

// SinkCredentials provides authorization for notifications.
type SinkCredentials struct {
	CredentialType       string `json:"credentialType,omitempty"` // e.g., "ACCESSTOKEN"
	AccessToken          string `json:"accessToken,omitempty"`
	AccessTokenExpireUtc string `json:"accessTokenExpireUtc,omitempty"` // RFC 3339 date-time
	AccessTokenType      string `json:"accessTokenType,omitempty"`      // e.g., "bearer"
}
