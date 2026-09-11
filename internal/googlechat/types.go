package googlechat

// Space represents a Google Chat space or 1:1 DM.
type Space struct {
	Name            string `json:"name,omitempty"`
	Type            string `json:"type,omitempty"`
	SpaceType       string `json:"spaceType,omitempty"`
	SingleUserBotDm bool   `json:"singleUserBotDm,omitempty"`
	DisplayName     string `json:"displayName,omitempty"`
}

// User represents a Google Chat user or bot.
type User struct {
	Name        string `json:"name,omitempty"` // e.g. "users/123456789"
	DisplayName string `json:"displayName,omitempty"`
	AvatarUrl   string `json:"avatarUrl,omitempty"`
	Email       string `json:"email,omitempty"`
	Type        string `json:"type,omitempty"` // "HUMAN" or "BOT"
}

// Thread represents a message thread in Google Chat.
type Thread struct {
	Name      string `json:"name,omitempty"`
	ThreadKey string `json:"threadKey,omitempty"`
}

// Message represents a Google Chat message.
type Message struct {
	Name         string   `json:"name,omitempty"` // e.g. "spaces/SPACE_ID/messages/MESSAGE_ID"
	Text         string   `json:"text,omitempty"`
	Sender       *User    `json:"sender,omitempty"`
	Thread       *Thread  `json:"thread,omitempty"`
	Space        *Space   `json:"space,omitempty"`
	CardsV2      []CardV2 `json:"cardsV2,omitempty"`
	ArgumentText string   `json:"argumentText,omitempty"`
}

// CardV2 wraps a Card with a cardId.
type CardV2 struct {
	CardID string `json:"cardId,omitempty"`
	Card   *Card  `json:"card,omitempty"`
}

// Card represents a Google Chat card.
type Card struct {
	Header   *CardHeader `json:"header,omitempty"`
	Sections []Section   `json:"sections,omitempty"`
}

// CardHeader is the top banner of a card.
type CardHeader struct {
	Title     string `json:"title,omitempty"`
	Subtitle  string `json:"subtitle,omitempty"`
	ImageURL  string `json:"imageUrl,omitempty"`
	ImageType string `json:"imageType,omitempty"` // "SQUARE" or "CIRCLE"
}

// Section is a visual container inside a Card.
type Section struct {
	Header      string   `json:"header,omitempty"`
	Widgets     []Widget `json:"widgets,omitempty"`
	Collapsible bool     `json:"collapsible,omitempty"`
}

// Widget represents a UI element inside a Section.
type Widget struct {
	TextParagraph *TextParagraph `json:"textParagraph,omitempty"`
	DecoratedText *DecoratedText `json:"decoratedText,omitempty"`
	ButtonList    *ButtonList    `json:"buttonList,omitempty"`
	Divider       *Divider       `json:"divider,omitempty"`
}

// TextParagraph displays text with basic markdown support.
type TextParagraph struct {
	Text string `json:"text,omitempty"`
}

// DecoratedText displays text with icons, labels, or buttons.
type DecoratedText struct {
	TopLabel    string `json:"topLabel,omitempty"`
	Text        string `json:"text,omitempty"`
	BottomLabel string `json:"bottomLabel,omitempty"`
	StartIcon   *Icon  `json:"startIcon,omitempty"`
}

// Divider renders a horizontal rule.
type Divider struct{}

// ButtonList holds a group of buttons.
type ButtonList struct {
	Buttons []Button `json:"buttons,omitempty"`
}

// Button is an interactive button.
type Button struct {
	Text     string   `json:"text,omitempty"`
	Icon     *Icon    `json:"icon,omitempty"`
	OnClick  *OnClick `json:"onClick,omitempty"`
	Disabled bool     `json:"disabled,omitempty"`
}

// Icon specifies a known material icon or image URL.
type Icon struct {
	KnownIcon string `json:"knownIcon,omitempty"`
	IconURL   string `json:"iconUrl,omitempty"`
}

// OnClick defines what happens when an element is clicked.
type OnClick struct {
	Action   *Action   `json:"action,omitempty"`
	OpenLink *OpenLink `json:"openLink,omitempty"`
}

// Action triggers a CARD_CLICKED event back to the app.
type Action struct {
	Function   string            `json:"function,omitempty"`
	Parameters []ActionParameter `json:"parameters,omitempty"`
}

// ActionParameter is a key-value parameter passed to an Action.
type ActionParameter struct {
	Key   string `json:"key,omitempty"`
	Value string `json:"value,omitempty"`
}

// OpenLink opens a URL.
type OpenLink struct {
	URL string `json:"url,omitempty"`
}

// EventType identifies the kind of interaction event from Google Chat.
type EventType string

const (
	EventMessage          EventType = "MESSAGE"
	EventCardClicked      EventType = "CARD_CLICKED"
	EventAddedToSpace     EventType = "ADDED_TO_SPACE"
	EventRemovedFromSpace EventType = "REMOVED_FROM_SPACE"
)

// Event represents an inbound interaction event from Google Chat.
type Event struct {
	Type      EventType   `json:"type,omitempty"`
	EventTime string      `json:"eventTime,omitempty"`
	Space     *Space      `json:"space,omitempty"`
	Message   *Message    `json:"message,omitempty"`
	User      *User       `json:"user,omitempty"`
	Action    *FormAction `json:"action,omitempty"`
}

// FormAction represents the action payload in a CARD_CLICKED event.
type FormAction struct {
	ActionMethodName string            `json:"actionMethodName,omitempty"`
	Parameters       []ActionParameter `json:"parameters,omitempty"`
}

// ParameterMap returns Action parameters as a map for convenient lookup.
func (a *FormAction) ParameterMap() map[string]string {
	if a == nil {
		return nil
	}
	res := make(map[string]string, len(a.Parameters))
	for _, p := range a.Parameters {
		res[p.Key] = p.Value
	}
	return res
}
