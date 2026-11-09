//go:build kyse

package admin

import (
	"github.com/arandu-io/kyse/components"

	agenda "github.com/hyz-is/arandu-agenda"
)

@go
// ServiceFormData is what the handler hands this page.
type ServiceFormData = agenda.ServiceFormPageData
@endgo

@extends('layouts.app')

@section('content')
<section class="mx-auto w-full max-w-2xl min-w-0 px-4 py-8 sm:px-6 lg:px-8">
	<p class="text-sm"><a class="text-muted-foreground hover:underline" href="{{ .BackURL }}">{{ .Labels.T("control.back") }}</a></p>
	<header class="mt-3">
		@if(.Editing)
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.service_edit_title") }}</h1>
		@else
			<h1 class="text-2xl font-semibold tracking-tight">{{ .Labels.T("screen.service_new_title") }}</h1>
		@endif
		<p class="text-muted-foreground mt-1 max-w-prose text-sm">{{ .Labels.T("screen.service_lead") }}</p>
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
		{!! components.Field(components.FieldProps{Name: "key", Label: .Labels.T("field.key"), Hint: .Labels.T("screen.key_hint"), Value: .Key, Required: true, Autofocus: true, Page: .Form()}) !!}
		{!! components.Field(components.FieldProps{Name: "name", Label: .Labels.T("field.name"), Value: .Name, Required: true, Page: .Form()}) !!}
		{!! components.Textarea(components.TextareaProps{Name: "description", Label: .Labels.T("field.description"), Value: .Description, Rows: 3, Page: .Form()}) !!}
		{!! components.Field(components.FieldProps{Name: "duration_minutes", Label: .Labels.T("field.duration_minutes"), Hint: .Labels.T("screen.duration_hint"), Value: .Duration, Required: true, Page: .Form()}) !!}
		{!! components.Checkbox(components.CheckboxProps{Name: "requires_payment", ID: "requires-payment", Label: .Labels.T("field.requires_payment"), Hint: .Labels.T("screen.requires_payment_hint"), Checked: .RequiresPayment, Page: .Form()}) !!}
		{!! components.Checkbox(components.CheckboxProps{Name: "active", ID: "service-active", Label: .Labels.T("field.active"), Hint: .Labels.T("screen.active_hint"), Checked: .Active, Page: .Form()}) !!}
		<div class="flex flex-wrap items-center gap-2">
			{!! components.Button(components.ButtonProps{Label: .Labels.T("control.save"), Type: "submit"}) !!}
			<a class="btn" data-variant="outline" href="{{ .BackURL }}">{{ .Labels.T("control.cancel") }}</a>
		</div>
	</form>
</section>
@endsection
