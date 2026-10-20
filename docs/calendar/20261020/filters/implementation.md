# Calendar filters and readable views — 2026-10-20

Upcoming and the calendar grid now have independent source selections and text
search. Each section has a collapsed filter panel with an active-filter badge.
Source choices persist per browser; the old shared choice seeds both sections.
Explicit query parameters take precedence. Text queries stay in navigation and
form return URLs, not cookies. Search matches all supplied words across title,
location, description and calendar name, using effective recurrence fields.
Past 30 Days uses the list filters; Deleted Events remains separate.

Upcoming defaults to today plus the following six days. Controls offer 7, 14,
30 and 90 days; Show next 30 days extends the chosen horizon. Browsing other
calendar dates does not move Upcoming's anchor. Long descriptions and Past
30 Days start collapsed to keep the list manageable.

Month uses the available workspace width, reduced padding, taller day cells and
up to four lines of each title before truncation. Keyboard focus reveals the full
title; event details remain available on click. Three entries and the Day-view
overflow link are retained. Narrow screens use abbreviated weekday labels;
Day remains the detailed phone view, and Week scrolls inside its section.

English and Spanish controls and guidance are included. Forms and disclosures
work without JavaScript. No storage migration, subscription mutation or CalDAV
format change is needed.

See [validation](validation.txt), [browser checks](browser.json), and the retained
[desktop](desktop-month-readable.png) and [phone](phone-month-readable.png) views.
Deployment evidence is recorded separately after installation.
