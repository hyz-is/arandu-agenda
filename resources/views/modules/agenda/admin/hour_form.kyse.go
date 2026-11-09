//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// HourFormData is what the handler hands this page.
type HourFormData = agenda.HourFormPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>
	<header class="mt-3">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.hour_new_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.hour_lead") }}</p>
	</header>

	<form class="card mt-6 grid gap-4 p-5" method="post" action="{{ .Action }}">
		@csrf
		@if(.HasErrors())
			<ul class="text-destructive grid gap-1 text-sm" role="alert">
				@foreach(.All() as line)
					<li>{{ line }}</li>
				@endforeach
			</ul>
		@endif
		{!! components.Select(components.SelectProps{ID: "hour-weekday", Name: "weekday", Label: .Labels.T("field.weekday"), Value: .Weekday, Options: .WeekdayOptions}) !!}
		{!! .TimePicker(components.DateTimePickerProps{Name: "from", ID: "hour-start", Label: .Labels.T("field.from"), Value: .Start, Required: true, Page: .Form()}) !!}
		{!! .TimePicker(components.DateTimePickerProps{Name: "to", ID: "hour-end", Label: .Labels.T("field.to"), Value: .End, Required: true, Page: .Form()}) !!}
		<div class="flex flex-wrap items-center gap-2">
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}
			<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
		</div>
	</form>
</section>
@endsection
