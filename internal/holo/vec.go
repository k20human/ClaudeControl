// Package holo computes the particle sphere. It has no dependency on a
// terminal, so every part of it is testable on its own.
package holo

import "math"

// Vec is a point or a direction in three dimensions.
type Vec struct{ X, Y, Z float64 }

// Add returns a + b.
func (a Vec) Add(b Vec) Vec { return Vec{a.X + b.X, a.Y + b.Y, a.Z + b.Z} }

// Scale returns a scaled by s.
func (a Vec) Scale(s float64) Vec { return Vec{a.X * s, a.Y * s, a.Z * s} }

// Dot returns the scalar product.
func (a Vec) Dot(b Vec) float64 { return a.X*b.X + a.Y*b.Y + a.Z*b.Z }

// Cross returns the vector product.
func (a Vec) Cross(b Vec) Vec {
	return Vec{
		a.Y*b.Z - a.Z*b.Y,
		a.Z*b.X - a.X*b.Z,
		a.X*b.Y - a.Y*b.X,
	}
}

// Len returns the length.
func (a Vec) Len() float64 { return math.Sqrt(a.Dot(a)) }

// Unit returns a with length one, or the Z axis for a zero vector.
func (a Vec) Unit() Vec {
	l := a.Len()
	if l == 0 {
		return Vec{Z: 1}
	}
	return a.Scale(1 / l)
}

// RotateY turns a about the vertical axis.
func RotateY(a Vec, angle float64) Vec {
	c, s := math.Cos(angle), math.Sin(angle)
	return Vec{X: a.X*c + a.Z*s, Y: a.Y, Z: -a.X*s + a.Z*c}
}

// RotateX tilts a about the horizontal axis.
func RotateX(a Vec, angle float64) Vec {
	c, s := math.Cos(angle), math.Sin(angle)
	return Vec{X: a.X, Y: a.Y*c - a.Z*s, Z: a.Y*s + a.Z*c}
}
