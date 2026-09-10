package emaildomain

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

type stubOverrides struct {
	blocked, allowed []string
	err              error
	calls            int
}

func (s *stubOverrides) Load(ctx context.Context) ([]string, []string, error) {
	s.calls++
	return s.blocked, s.allowed, s.err
}

func TestChecker_IsDisposable(t *testing.T) {
	ctx := context.Background()

	t.Run("la lista embebida atrapa los dominios conocidos", func(t *testing.T) {
		c := NewChecker(nil, nil)

		// Los dos que originaron esto, en septiembre de 2026.
		assert.True(t, c.IsDisposable(ctx, "vomec28625@novelv.com"))
		assert.True(t, c.IsDisposable(ctx, "rikisey853@kolsea.com"))
	})

	t.Run("no toca proveedores legítimos ni dominios de clientes", func(t *testing.T) {
		c := NewChecker(nil, nil)

		for _, email := range []string{
			"alguien@gmail.com", "alguien@outlook.com", "alguien@hotmail.com",
			"alguien@yahoo.com", "alguien@icloud.com",
			"humberto.osorio@dicipacs.com", "ecvega@notaria230.com.mx",
			"scervantes@maximage-ds.com", "erandini.brizuela@kiban.com",
		} {
			assert.False(t, c.IsDisposable(ctx, email), email)
		}
	})

	// Es la razón de ser de la capa de Mongo: bloquear hoy lo que la lista
	// pública todavía no conoce.
	t.Run("un bloqueo manual atrapa lo que la lista pública no trae", func(t *testing.T) {
		nuevo := "dominio-que-no-existe-en-ninguna-lista.com"
		sinOverride := NewChecker(nil, nil)
		assert.False(t, sinOverride.IsDisposable(ctx, "a@"+nuevo))

		c := NewChecker(&stubOverrides{blocked: []string{nuevo}}, nil)
		assert.True(t, c.IsDisposable(ctx, "a@"+nuevo))
	})

	// Y la otra mitad: la lista pública agrega de muchas fuentes y puede
	// traer un dominio legítimo. Sin salida de emergencia, ese cliente no
	// tiene forma de entrar.
	t.Run("un permiso manual desbloquea un falso positivo de la lista", func(t *testing.T) {
		c := NewChecker(&stubOverrides{allowed: []string{"novelv.com"}}, nil)

		assert.False(t, c.IsDisposable(ctx, "cliente@novelv.com"))
	})

	t.Run("el permiso le gana al bloqueo manual", func(t *testing.T) {
		c := NewChecker(&stubOverrides{
			blocked: []string{"ejemplo.com"},
			allowed: []string{"ejemplo.com"},
		}, nil)

		assert.False(t, c.IsDisposable(ctx, "a@ejemplo.com"))
	})

	// Si la tienda de overrides no responde, sigue valiendo la lista
	// embebida: cortar todos los registros por eso sería peor que dejar
	// pasar un correo desechable.
	t.Run("si los overrides fallan responde con la lista embebida y avisa", func(t *testing.T) {
		var reported error
		c := NewChecker(&stubOverrides{err: errors.New("mongo caído")}, func(err error) { reported = err })

		assert.True(t, c.IsDisposable(ctx, "a@novelv.com"))
		assert.False(t, c.IsDisposable(ctx, "a@gmail.com"))
		assert.Error(t, reported)
	})

	t.Run("no distingue mayúsculas ni espacios", func(t *testing.T) {
		c := NewChecker(&stubOverrides{blocked: []string{"  Ejemplo.COM  "}}, nil)

		assert.True(t, c.IsDisposable(ctx, "A@NOVELV.COM"))
		assert.True(t, c.IsDisposable(ctx, "a@ejemplo.com"))
	})

	// Un correo malformado no es "desechable": rechazarlo es tarea del
	// validador de formato, y contestar que sí acá le mostraría al usuario
	// el error equivocado.
	t.Run("un correo sin dominio usable no es desechable", func(t *testing.T) {
		c := NewChecker(nil, nil)

		for _, email := range []string{"", "sin-arroba", "termina-en@", "@sin-local"} {
			assert.False(t, c.IsDisposable(ctx, email), email)
		}
	})

	t.Run("con varias arrobas toma el dominio de la última", func(t *testing.T) {
		c := NewChecker(nil, nil)

		assert.True(t, c.IsDisposable(ctx, "raro@cosa@novelv.com"))
	})
}

func TestDomainOf(t *testing.T) {
	assert.Equal(t, "gmail.com", DomainOf("Alguien@GMAIL.com"))
	assert.Equal(t, "gmail.com", DomainOf("alguien@ gmail.com "))
	assert.Empty(t, DomainOf("sin-arroba"))
	assert.Empty(t, DomainOf("vacio@"))
}

// Un embed truncado dejaría la protección en nada sin que nada falle, así
// que el tamaño de la lista es en sí mismo una aserción.
func TestEmbeddedCount(t *testing.T) {
	assert.Greater(t, EmbeddedCount(), 70000, "la lista embebida se ve truncada")
}
