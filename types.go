package main

import (
	"encoding/json"
	"time"
)

type BoardView struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	TenantID       string          `json:"tenantId"`
	Name           string          `json:"name"`
	Description    *string         `json:"description"`
	KeyPrefix      string          `json:"keyPrefix"`
	IsPublished    bool            `json:"isPublished"`
	CardSchema     json.RawMessage `json:"cardSchema"`
	Settings       json.RawMessage `json:"settings"`
	CreatedBy      *string         `json:"createdBy"`
	CreatedAt      time.Time       `json:"createdAt"`
	UpdatedAt      time.Time       `json:"updatedAt"`
	DeletedAt      *time.Time      `json:"deletedAt,omitempty"`
	States         []StateView     `json:"states,omitempty"`
}

type StateView struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	TenantID       string     `json:"tenantId"`
	BoardID        string     `json:"boardId"`
	Name           string     `json:"name"`
	Position       int        `json:"position"`
	IsDefault      bool       `json:"isDefault"`
	WIPLimit       *int       `json:"wipLimit"`
	Color          *string    `json:"color"`
	CreatedBy      *string    `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type TicketView struct {
	ID              string          `json:"id"`
	OrganizationID  string          `json:"organizationId"`
	TenantID        string          `json:"tenantId"`
	BoardID         string          `json:"boardId"`
	StateID         string          `json:"stateId"`
	ParentTicketID  *string         `json:"parentTicketId"`
	Number          int             `json:"number"`
	Key             string          `json:"key"`
	Title           string          `json:"title"`
	Body            *string         `json:"body"`
	CardData        json.RawMessage `json:"cardData"`
	TicketType      string          `json:"ticketType"`
	Priority        int             `json:"priority"`
	Position        int             `json:"position"`
	DueAt           *time.Time      `json:"dueAt"`
	EstimateMinutes *int            `json:"estimateMinutes"`
	LoggedMinutes   int             `json:"loggedMinutes"`
	CompletedAt     *time.Time      `json:"completedAt"`
	CreatedBy       *string         `json:"createdBy"`
	CreatedAt       time.Time       `json:"createdAt"`
	UpdatedAt       time.Time       `json:"updatedAt"`
	DeletedAt       *time.Time      `json:"deletedAt,omitempty"`

	// Tags is populated on reads (list and get) so a board can render tag
	// chips without a request per card. Omitted when the ticket has none.
	Tags []TagView `json:"tags,omitempty"`
}

type MemberView struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	TenantID       string    `json:"tenantId"`
	BoardID        string    `json:"boardId"`
	UserID         string    `json:"userId"`
	Role           string    `json:"role"`
	CreatedBy      *string   `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type PersonLinkView struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	TenantID       string    `json:"tenantId"`
	TicketID       string    `json:"ticketId"`
	UserID         string    `json:"userId"`
	CreatedBy      *string   `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
}

// MessageView is one entry in the shared agent/human thread (CW-18).
// TicketID is nil for the board-level thread.
type MessageView struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	TenantID       string     `json:"tenantId"`
	BoardID        string     `json:"boardId"`
	TicketID       *string    `json:"ticketId"`
	ActorType      string     `json:"actorType"`
	ActorKey       string     `json:"actorKey"`
	DisplayName    string     `json:"displayName"`
	Body           string     `json:"body"`
	CreatedBy      *string    `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`

	// Attachments arrive with the thread so a client rendering N messages does
	// not issue N requests for files. Omitted when the message has none.
	Attachments []AttachmentView `json:"attachments,omitempty"`
}

type TagView struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	TenantID       string     `json:"tenantId"`
	Name           string     `json:"name"`
	Kind           string     `json:"kind"`
	Color          *string    `json:"color"`
	CreatedBy      *string    `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type CommentView struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	TenantID       string     `json:"tenantId"`
	TicketID       string     `json:"ticketId"`
	Body           string     `json:"body"`
	CreatedBy      *string    `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type LinkView struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	TenantID       string     `json:"tenantId"`
	TicketID       string     `json:"ticketId"`
	URL            string     `json:"url"`
	Title          *string    `json:"title"`
	LinkType       string     `json:"linkType"`
	CreatedBy      *string    `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
	DeletedAt      *time.Time `json:"deletedAt,omitempty"`
}

type AttachmentView struct {
	ID             string `json:"id"`
	OrganizationID string `json:"organizationId"`
	TenantID       string `json:"tenantId"`
	// TicketID is legacy and set only on ticket attachments created before
	// CW-19. ParentType/ParentID are the source of truth.
	TicketID    *string    `json:"ticketId"`
	ParentType  string     `json:"parentType"`
	ParentID    string     `json:"parentId"`
	FileID      string     `json:"fileId"`
	Filename    *string    `json:"filename"`
	ContentType *string    `json:"contentType"`
	SizeBytes   *int64     `json:"sizeBytes"`
	CreatedBy   *string    `json:"createdBy"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	DeletedAt   *time.Time `json:"deletedAt,omitempty"`
}

type ChecklistView struct {
	ID             string              `json:"id"`
	OrganizationID string              `json:"organizationId"`
	TenantID       string              `json:"tenantId"`
	TicketID       string              `json:"ticketId"`
	Title          string              `json:"title"`
	Position       int                 `json:"position"`
	CreatedBy      *string             `json:"createdBy"`
	CreatedAt      time.Time           `json:"createdAt"`
	UpdatedAt      time.Time           `json:"updatedAt"`
	Items          []ChecklistItemView `json:"items,omitempty"`
}

type ChecklistItemView struct {
	ID             string    `json:"id"`
	OrganizationID string    `json:"organizationId"`
	TenantID       string    `json:"tenantId"`
	ChecklistID    string    `json:"checklistId"`
	Title          string    `json:"title"`
	IsDone         bool      `json:"isDone"`
	Position       int       `json:"position"`
	CreatedBy      *string   `json:"createdBy"`
	CreatedAt      time.Time `json:"createdAt"`
	UpdatedAt      time.Time `json:"updatedAt"`
}

type TimeEntryView struct {
	ID             string     `json:"id"`
	OrganizationID string     `json:"organizationId"`
	TenantID       string     `json:"tenantId"`
	TicketID       string     `json:"ticketId"`
	UserID         string     `json:"userId"`
	StartedAt      *time.Time `json:"startedAt"`
	EndedAt        *time.Time `json:"endedAt"`
	Minutes        int        `json:"minutes"`
	Note           *string    `json:"note"`
	CreatedBy      *string    `json:"createdBy"`
	CreatedAt      time.Time  `json:"createdAt"`
	UpdatedAt      time.Time  `json:"updatedAt"`
}

type ActivityView struct {
	ID             string          `json:"id"`
	OrganizationID string          `json:"organizationId"`
	TenantID       string          `json:"tenantId"`
	BoardID        *string         `json:"boardId"`
	TicketID       *string         `json:"ticketId"`
	ActorID        *string         `json:"actorId"`
	Action         string          `json:"action"`
	Payload        json.RawMessage `json:"payload"`
	CreatedAt      time.Time       `json:"createdAt"`
}
