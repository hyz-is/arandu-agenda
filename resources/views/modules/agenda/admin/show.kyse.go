//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// ShowData is what the handler hands this page.
type ShowData = agenda.TeamShowPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-5xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3 flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<p class="text-muted-foreground text-sm">{{ .Booking.Code }} · {{ .Booking.Space }}</p>
			<h1 class="text-2xl font-semibold tracking-tight break-words">{{ .Booking.Service }}</h1>
		</div>
		{!! components.Badge(components.BadgeProps{Label: .Booking.Status, Variant: agenda.StatusVariant(.Booking.StatusKey)}) !!}
	</header>

	@if(.HasErrors())
		<ul class="text-destructive mt-4 grid gap-1 text-sm" role="alert">
			@foreach(.All() as line)
				<li>{{ line }}</li>
			@endforeach
		</ul>
	@endif

	<div class="mt-6 grid min-w-0 gap-6 lg:grid-cols-3">
		<dl class="card grid min-w-0 content-start gap-3 p-5 text-sm lg:col-span-2 sm:grid-cols-2">
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.day") }}</dt><dd>{{ .Booking.Day }}</dd></div>
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.time") }}</dt><dd>{{ .Booking.Time }} – {{ .Booking.Ends }}</dd></div>
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.requester") }}</dt><dd class="break-words">{{ .Booking.Requester }}</dd></div>
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.space") }}</dt><dd class="break-all">{{ .Booking.Space }}</dd></div>
			@if(.Booking.Subject != "")
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.subject") }}</dt><dd class="break-words">{{ .Booking.Subject }} ({{ .Booking.SubjectType }} {{ .Booking.SubjectID }})</dd></div>
			@endif
			@if(.Booking.PaymentRef != "")
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.payment_ref") }}</dt><dd class="break-all">{{ .Booking.PaymentRef }}</dd></div>
			@endif
			@if(.Booking.HoldUntil != "")
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.hold_until") }}</dt><dd>{{ .Booking.HoldUntil }}</dd></div>
			@endif
			@if(.Booking.ReleasedBy != "")
				<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.released") }}</dt><dd class="break-words">{{ .Booking.ReleasedBy }}: {{ .Booking.ReleasedReason }}</dd></div>
			@endif
			@if(.Booking.Note != "")
				<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.customer_note") }}</dt><dd class="break-words">{{ .Booking.Note }}</dd></div>
			@endif
			@if(.Booking.Confirmed != "")
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.confirmed_at") }}</dt><dd>{{ .Booking.Confirmed }}</dd></div>
			@endif
			@if(.Booking.Reminded != "")
				<div><dt class="text-muted-foreground">{{ .Labels.T("field.reminded_at") }}</dt><dd>{{ .Booking.Reminded }}</dd></div>
			@endif
			@if(.Booking.Completed != "")
				<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.completed") }}</dt><dd class="break-words">{{ .Booking.Completed }} · {{ .Booking.CompletedBy }}</dd></div>
			@endif
			@if(.Booking.TeamNote != "")
				<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.team_note") }}</dt><dd class="break-words">{{ .Booking.TeamNote }}</dd></div>
			@endif
			@if(.Booking.CancelReason != "")
				<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.cancel_reason") }}</dt><dd class="break-words">{{ .Booking.CancelReason }}</dd></div>
			@endif
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.reschedules") }}</dt><dd>{{ .Booking.Reschedules }}</dd></div>
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.created_at") }}</dt><dd>{{ .Booking.Created }}</dd></div>
		</dl>

		<aside class="grid min-w-0 content-start gap-4" aria-label="{{ .Labels.T("screen.details") }}">
			@if(.CanComplete)
				<form class="card grid gap-3 p-4" method="post" action="{{ .CompleteURL }}">
					@csrf
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.complete_title") }}</h2>
					{!! components.Textarea(components.TextareaProps{ID: "complete-note", Name: "note", Label: .Labels.T("field.team_note"), Hint: .Labels.T("screen.team_note_hint"), Rows: 3}) !!}
					<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.complete"), Size: "sm", Type: "submit"}) !!}</div>
				</form>
			@endif
			@if(.CanRelease)
				<form class="card grid gap-3 p-4" method="post" action="{{ .ReleaseURL }}">
					@csrf
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.release_title") }}</h2>
					<p class="text-muted-foreground -mt-1 text-sm">{{ .Labels.T("screen.release_lead") }}</p>
					{!! components.Textarea(components.TextareaProps{ID: "release-reason", Name: "reason", Label: .Labels.T("field.reason"), Rows: 2, Required: true}) !!}
					<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.release"), Variant: "outline", Size: "sm", Type: "submit"}) !!}</div>
				</form>
			@endif
			@if(.CanNoShow)
				<form class="card grid gap-3 p-4" method="post" action="{{ .NoShowURL }}">
					@csrf
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.no_show_title") }}</h2>
					<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.no_show"), Variant: "outline", Size: "sm", Type: "submit"}) !!}</div>
				</form>
			@endif
			@if(.CanReschedule)
				<form class="card grid gap-3 p-4" method="post" action="{{ .RescheduleURL }}">
					@csrf
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.reschedule_title") }}</h2>
					<p class="text-muted-foreground -mt-1 text-sm">{{ .Labels.T("screen.team_reschedule_lead") }}</p>
					{!! .DatePicker(components.DateTimePickerProps{Name: "day", ID: "team-day", Label: .Labels.T("field.day"), Value: .Day, Min: .MinDay, Required: true}) !!}
					{!! .TimePicker(components.DateTimePickerProps{Name: "time", ID: "team-time", Label: .Labels.T("field.time"), Value: .Time, Required: true}) !!}
					<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.reschedule"), Variant: "outline", Size: "sm", Type: "submit"}) !!}</div>
				</form>
			@endif
			@if(.CanCancel)
				<form class="card grid gap-3 p-4" method="post" action="{{ .CancelURL }}">
					@csrf
					<h2 class="text-base font-semibold">{{ .Labels.T("screen.cancel_title") }}</h2>
					{!! components.Textarea(components.TextareaProps{ID: "team-cancel-reason", Name: "reason", Label: .Labels.T("field.reason"), Rows: 2}) !!}
					<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.cancel_booking"), Variant: "outline", Size: "sm", Type: "submit"}) !!}</div>
				</form>
			@endif
		</aside>
	</div>
</section>
@endsection
