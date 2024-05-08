package dependencies

import "fmt"

var (
	JINJA_PRELOAD_TEMPLATE = `{% set fruits = ['Apple'] %}
{{ 'a' }}
{% for fruit in fruits %}
	<li>{{ fruit }}</li>
{% endfor %}
{% if fruits|length > 1 %}
1
{% endif %}
{% for i in range(5) %}
	{% if i == 3 %}{{ i }}{% else %}{% endif %}
{% endfor %}
	{% for i in range(3) %}
		{{ i + 1 }}
	{% endfor %}
{% macro say_hello() %}a{{ 'b' }}{% endmacro %}
{{ s }}{{ say_hello() }}`
)

func init() {
	SetupDependency("jinja2", "", fmt.Sprintf(`import jinja2
def _jinja2_preload_():
	# prepare jinja2 environment, load template and render before to avoid sandbox issue
	template = jinja2.Template('''%s''')
	template.render(s='a')

if __name__ == '__main__':
	_jinja2_preload_()
	`, JINJA_PRELOAD_TEMPLATE,
	))
}
