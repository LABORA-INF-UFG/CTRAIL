package nef

// TrafficInfluSub represents 3GPP TS 29.522 Traffic Influence Subscription
type TrafficInfluSub struct {
	SubscriptionId          string               `json:"subscriptionId,omitempty"`          //
	AfAppId                 string               `json:"afAppId,omitempty"`                 //
	AfServiceId             string               `json:"afServiceId,omitempty"`             //
	AfTransId               string               `json:"afTransId,omitempty"`               //
	TrafficFilters          []FlowInfo           `json:"trafficFilters,omitempty"`          //
	EthTrafficFilters       []EthFlowDescription `json:"ethTrafficFilters,omitempty"`       //
	Dnn                     string               `json:"dnn,omitempty"`                     //
	Snssai                  *Snssai              `json:"snssai,omitempty"`                  //
	NotificationDestination string               `json:"notificationDestination,omitempty"` //
	WebsockNotifConfig      *WebsockNotifConfig  `json:"websockNotifConfig,omitempty"`      //
	SubscribedEvents        []SubscribedEvent    `json:"subscribedEvents,omitempty"`        //
	AppReloInd              *bool                `json:"appReloInd,omitempty"`              //
	AnyUeInd                *bool                `json:"anyUeInd,omitempty"`                //
	Gpsi                    string               `json:"gpsi,omitempty"`                    //
	Ipv4Addr                string               `json:"ipv4Addr,omitempty"`                //
	Ipv6Addr                string               `json:"ipv6Addr,omitempty"`                //
	TrafficRoutes           []TrafficRoute       `json:"trafficRoutes,omitempty"`
	Self                    string               `json:"self,omitempty"`                    // Self-referencing URL
}

type TrafficRoute struct {
	Dnai string `json:"dnai"` // Data Network Access Identifier
}

type WebsockNotifConfig struct {
	WebsocketUri string `json:"websocketUri,omitempty"` //
}

type FlowInfo struct {
	FlowId           *int     `json:"flowId,omitempty"`
	FlowDescriptions []string `json:"flowDescriptions,omitempty"`

	Ipv4Addr   string `json:"ipv4Addr,omitempty"`   // - May be redundant
	Ipv6Addr   string `json:"ipv6Addr,omitempty"`   // - May be redundant
	PortNumber int    `json:"portNumber,omitempty"` // - May be redundant
	Protocol   string `json:"protocol,omitempty"`   // May be redundant
}

// EthFlowDescription struct remains the same.
type EthFlowDescription struct {
	DestMacAddr string `json:"destMacAddr,omitempty"` //
	EthType     string `json:"ethType,omitempty"`     //
}

// Snssai struct remains the same.
type Snssai struct {
	Sst int    `json:"sst"`          //
	Sd  string `json:"sd,omitempty"` //
}

// SubscribedEvent defines events the AF can subscribe to.
type SubscribedEvent struct {
	Event string `json:"event"` // e.g., "UP_PATH_CHANGE"
	// Additional parameters for the event if any
}
