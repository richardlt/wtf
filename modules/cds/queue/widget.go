package cdsqueue

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/bep/debounce"
	"strconv"
	"time"

	"github.com/ovh/cds/sdk"
	"github.com/ovh/cds/sdk/cdsclient"
	"github.com/rivo/tview"
	"github.com/wtfutil/wtf/utils"
	"github.com/wtfutil/wtf/view"
)

// Widget define wtf widget to register widget later
type Widget struct {
	view.MultiSourceWidget
	view.KeyboardWidget
	view.TextWidget

	filters []string

	client cdsclient.Interface

	settings *Settings
	Selected int
	maxItems int
	Items    []sdk.WorkflowNodeJobRun
}

// NewWidget creates a new instance of the widget
func NewWidget(app *tview.Application, pages *tview.Pages, settings *Settings) *Widget {
	widget := Widget{
		KeyboardWidget:    view.NewKeyboardWidget(app, pages, settings.common),
		MultiSourceWidget: view.NewMultiSourceWidget(settings.common, "workflow", "workflows"),
		TextWidget:        view.NewTextWidget(app, settings.common),

		settings: settings,
	}

	widget.initializeKeyboardControls()
	widget.View.SetRegions(true)
	widget.View.SetInputCapture(widget.InputCapture)
	widget.SetDisplayFunction(widget.displayReload)

	widget.Unselect()
	widget.filters = []string{sdk.StatusWaiting, sdk.StatusBuilding}

	widget.KeyboardWidget.SetView(widget.View)

	widget.client = cdsclient.New(cdsclient.Config{
		Host:                              settings.apiURL,
		BuitinConsumerAuthenticationToken: settings.token,
	})

	ctx := context.Background()
	chanMessageReceived := make(chan sdk.WebsocketEvent)
	chanMessageToSend := make(chan []sdk.WebsocketFilter)

	go func() {
		widget.client.WebsocketEventsListen(ctx, chanMessageToSend, chanMessageReceived)
	}()

	chanMessageToSend <- []sdk.WebsocketFilter{{
		Type: sdk.WebsocketFilterTypeQueue,
	}}

	widget.Items, _ = widget.client.QueueWorkflowNodeJobRun(widget.currentFilter())

	debounced := debounce.New(500 * time.Millisecond)

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-chanMessageReceived:
				debounced(func() {
					if evt.Event.EventType != fmt.Sprintf("%T", sdk.EventRunWorkflowJob{}) {
						return
					}

					var e sdk.EventRunWorkflowJob
					if err := json.Unmarshal(evt.Event.Payload, &e); err != nil {
						return
					}

					if e.Status == sdk.StatusWaiting || e.Status == sdk.StatusBuilding {
						widget.Items, _ = widget.client.QueueWorkflowNodeJobRun(widget.currentFilter())
					} else {
						newItems := make([]sdk.WorkflowNodeJobRun, 0, len(widget.Items))
						for i := range widget.Items {
							if widget.Items[i].ID != e.ID {
								newItems = append(newItems, widget.Items[i])
							}
						}
						widget.Items = newItems
					}

					widget.Refresh()
				})
			}
		}
	}()

	config, _ := widget.client.ConfigUser()

	if config.URLUI != "" {
		widget.settings.uiURL = config.URLUI
	}

	return &widget
}

/* -------------------- Exported Functions -------------------- */

// SetItemCount sets the amount of workflows throughout the widgets display creation
func (widget *Widget) SetItemCount(items int) {
	widget.maxItems = items
}

// GetItemCount returns the amount of workflows calculated so far as an int
func (widget *Widget) GetItemCount() int {
	return widget.maxItems
}

// GetSelected returns the index of the currently highlighted item as an int
func (widget *Widget) GetSelected() int {
	if widget.Selected < 0 {
		return 0
	}
	return widget.Selected
}

// Next cycles the currently highlighted text down
func (widget *Widget) Next() {
	widget.Selected++
	if widget.Selected >= widget.maxItems {
		widget.Selected = 0
	}
	widget.View.Highlight(strconv.Itoa(widget.Selected)).ScrollToHighlight()
}

// Prev cycles the currently highlighted text up
func (widget *Widget) Prev() {
	widget.Selected--
	if widget.Selected < 0 {
		widget.Selected = widget.maxItems - 1
	}
	widget.View.Highlight(strconv.Itoa(widget.Selected)).ScrollToHighlight()
}

// Unselect stops highlighting the text and jumps the scroll position to the top
func (widget *Widget) Unselect() {
	widget.Selected = -1
	widget.View.Highlight()
	widget.View.ScrollToBeginning()
}

// Refresh reloads the data
func (widget *Widget) Refresh() {
	widget.display()
}

// HelpText displays the widgets controls
func (widget *Widget) HelpText() string {
	return widget.KeyboardWidget.HelpText()
}

/* -------------------- Unexported Functions -------------------- */

func (widget *Widget) currentFilter() string {
	if len(widget.filters) == 0 {
		return sdk.StatusWaiting
	}

	if widget.Idx < 0 || widget.Idx >= len(widget.filters) {
		widget.Idx = 0
		return sdk.StatusWaiting
	}

	return widget.filters[widget.Idx]
}

func (widget *Widget) openWorkflow() {
	currentSelection := widget.View.GetHighlights()
	if widget.Selected >= 0 && currentSelection[0] != "" {
		job := widget.Items[widget.Selected]
		prj := getVarsInPbj("cds.project", job.Parameters)
		workflow := getVarsInPbj("cds.workflow", job.Parameters)
		runNumber := getVarsInPbj("cds.run.number", job.Parameters)
		url := fmt.Sprintf("%s/project/%s/workflow/%s/run/%s", widget.settings.uiURL, prj, workflow, runNumber)
		utils.OpenFile(url)
	}
}
