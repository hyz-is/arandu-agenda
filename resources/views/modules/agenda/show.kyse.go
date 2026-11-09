//go:build kyse

package agenda

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// ShowData is what the handler hands this page.
type ShowData = agenda.ShowPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-3xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>

	<header class="mt-3 flex flex-wrap items-start justify-between gap-3">
		<div class="min-w-0">
			<p class="text-muted-foreground text-sm">{{ .Labels.T("field.code") }} {{ .Booking.Code }}</p>
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

	<dl class="card mt-6 grid gap-3 p-5 text-sm sm:grid-cols-2">
		<div><dt class="text-muted-foreground">{{ .Labels.T("field.day") }}</dt><dd>{{ .Booking.Day }}</dd></div>
		<div><dt class="text-muted-foreground">{{ .Labels.T("field.time") }}</dt><dd>{{ .Booking.Time }} – {{ .Booking.Ends }}</dd></div>
		@if(.Booking.Subject != "")
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.subject") }}</dt><dd class="break-words">{{ .Booking.Subject }}</dd></div>
		@endif
		@if(.Booking.Note != "")
			<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.note") }}</dt><dd class="break-words">{{ .Booking.Note }}</dd></div>
		@endif
		@if(.Booking.Confirmed != "")
			<div><dt class="text-muted-foreground">{{ .Labels.T("field.confirmed_at") }}</dt><dd>{{ .Booking.Confirmed }}</dd></div>
		@endif
		@if(.Booking.CancelReason != "")
			<div class="sm:col-span-2"><dt class="text-muted-foreground">{{ .Labels.T("field.cancel_reason") }}</dt><dd class="break-words">{{ .Booking.CancelReason }}</dd></div>
		@endif
	</dl>

	@if(.AwaitingPayment)
		<div class="card mt-6 grid gap-3 p-5" role="status">
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.awaiting_payment") }}</h2>
			<p class="text-muted-foreground text-sm">{{ .Labels.T("screen.awaiting_payment_lead") }} {{ .Booking.HoldUntil }}.</p>
			@if(.PaymentURL != "")
				<div><a class="btn" href="{{ .PaymentURL }}">{{ .Labels.T("control.pay") }}</a></div>
			@endif
		</div>
	@endif
	@if(.HoldExpired)
		<p class="card text-muted-foreground mt-6 p-5 text-sm" role="status">{{ .Labels.T("screen.hold_expired") }}</p>
	@endif
	@if(.ChangeClosed)
		<p class="text-muted-foreground mt-6 text-sm">{{ .Labels.T("screen.change_closed") }}</p>
	@endif

	<div class="mt-6 flex flex-wrap gap-2">
		@if(.CanConfirm)
			<form method="post" action="{{ .ConfirmURL }}">
				@csrf
				{!! components.Button(components.ButtonProps{Label: .Labels.T("control.confirm"), Type: "submit"}) !!}
			</form>
		@endif
		@if(.CanReschedule)
			<a class="btn" data-variant="outline" href="{{ .RescheduleURL }}">{{ .Labels.T("control.reschedule") }}</a>
		@endif
	</div>

	@if(.CanCancel)
		<form class="card mt-6 grid gap-4 p-5" method="post" action="{{ .CancelURL }}">
			@csrf
			<h2 class="text-base font-semibold">{{ .Labels.T("screen.cancel_title") }}</h2>
			{!! components.Textarea(components.TextareaProps{ID: "cancel-reason", Name: "reason", Label: .Labels.T("field.reason"), Rows: 2}) !!}
			<div>{!! components.Button(components.ButtonProps{Label: .Labels.T("control.cancel_booking"), Variant: "outline", Type: "submit"}) !!}</div>
		</form>
	@endif
</section>
@endsection
