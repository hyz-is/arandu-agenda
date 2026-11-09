//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// ClosureFormData is what the handler hands this page.
type ClosureFormData = agenda.ClosureFormPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>
	<header class="mt-3">
		<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.closure_new_title") }}</h1>
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.closure_lead") }}</p>
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
		<div class="grid gap-4 sm:grid-cols-2">
			{!! .DatePicker(components.DateTimePickerProps{Name: "from_day", ID: "closure-from-day", Label: .Labels.T("field.from_day"), Value: .FromDay, Required: true, Page: .Form()}) !!}
			{!! .TimePicker(components.DateTimePickerProps{Name: "from_time", ID: "closure-from-time", Label: .Labels.T("field.from_time"), Value: .FromTime, Required: true, Page: .Form()}) !!}
			{!! .DatePicker(components.DateTimePickerProps{Name: "to_day", ID: "closure-to-day", Label: .Labels.T("field.to_day"), Value: .ToDay, Required: true, Page: .Form()}) !!}
			{!! .TimePicker(components.DateTimePickerProps{Name: "to_time", ID: "closure-to-time", Label: .Labels.T("field.to_time"), Value: .ToTime, Required: true, Page: .Form()}) !!}
		</div>
		{!! components.Field(components.FieldProps{Name: "reason", Label: .Labels.T("field.reason"), Value: .Reason, Page: .Form()}) !!}
		<div class="flex flex-wrap items-center gap-2">
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}
			<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
		</div>
	</form>
</section>
@endsection
