D2    := d2
THEME := 0
FLAGS := --theme $(THEME) --center --pad 50

.PHONY: all overview workers

all: overview workers

overview: chart-overview.svg
workers:  workers-diagram.svg

chart-overview.svg: chart-overview.d2
	$(D2) $(FLAGS) $< $@

workers-diagram.svg: workers-diagram.d2
	$(D2) $(FLAGS) $< $@
