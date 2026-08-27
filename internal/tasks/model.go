package tasks

type Task struct {
	ID                     string  `json:"id"`
	Title                  *string `json:"title,omitempty"`
	Detail                 *string `json:"detail,omitempty"`
	Status                 string  `json:"status"`
	Importance             *string `json:"importance,omitempty"`
	DueTime                int64   `json:"due_time"`
	CompletedTime          int64   `json:"completed_time"`
	CompletedAt            *int64  `json:"completed_at,omitempty"`
	LastModified           int64   `json:"last_modified"`
	Recurrence             *string `json:"recurrence,omitempty"`
	Reminder               bool    `json:"reminder"`
	Links                  *string `json:"links,omitempty"`
	Deleted                bool    `json:"deleted"`
	ICalBlob               *string `json:"-"`
	CreatedAt              int64   `json:"created_at"`
	UpdatedAt              int64   `json:"updated_at"`
	ForestNoteNotebookID   *string `json:"forestnote_notebook_id,omitempty"`
	ForestNotePageID       *string `json:"forestnote_page_id,omitempty"`
	ForestNoteNotebookName *string `json:"forestnote_notebook_name,omitempty"`
	ForestNoteSource       *string `json:"forestnote_source,omitempty"`
}
