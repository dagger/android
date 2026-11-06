package main

import (
	"reflect"
	"sort"
	"testing"
)

func discovered(files map[string]string) []string {
	var classes []*classDecl
	for name, src := range files {
		kotlin := len(name) > 3 && name[len(name)-3:] == ".kt"
		classes = append(classes, parseFile(src, "test", kotlin)...)
	}
	var names []string
	for _, c := range testClasses(classes) {
		names = append(names, c.qualified())
	}
	sort.Strings(names)
	return names
}

func TestDiscoverJava(t *testing.T) {
	got := discovered(map[string]string{
		"Abstract.java": `package com.example;
import org.junit.jupiter.api.Test;
public abstract class AbstractTest { @Test void inherited() {} }`,
		"Inherited.java": `package com.example;
public class InheritedTest extends AbstractTest {}`,
		"Generic.java": `package com.example;
public class GenericTest extends com.example.AbstractTest implements java.util.function.Supplier<String> { public String get() { return ""; } }`,
		"Contract.java": `package com.example;
public interface Contract { @org.junit.jupiter.api.Test default void holds() {} }`,
		"Implementer.java": `package com.example;
public class ImplementerTest implements Contract {}`,
		"Parameterized.java": `package com.example;
/* class Commented { @Test void no() {} } */
@RunWith(Parameterized.class)
public class ParamTest {
  // @Test not really
  String s = "class Fake { @Test }";
  @Parameterized.Parameter public int value;
}`,
		"Outer.java": `package com.example;
public class OuterTest {
  @Test void top() {}
  @Nested class NestedTests { @Test void nested() {} }
  static class StaticTests { @Test void s() {} }
  class InnerHelper { @Test void ignored() {} }
  enum Mode { A, B }
}`,
		"Helper.java": `package com.example;
public class Helper { void run() { Runnable r = new Runnable() { public void run() {} }; Object o = Helper.class; } }`,
		"Legacy.java": `package com.example;
import junit.framework.TestCase;
public class LegacyTest extends TestCase { public void testIt() {} }`,
		"Annotation.java": `package com.example;
public @interface Marker { }`,
		"Record.java": `package com.example;
public record Pair(int a, int b) { }`,
	})
	want := []string{
		"com.example.GenericTest",
		"com.example.ImplementerTest",
		"com.example.InheritedTest",
		"com.example.LegacyTest",
		"com.example.OuterTest",
		"com.example.OuterTest$NestedTests",
		"com.example.OuterTest$StaticTests",
		"com.example.ParamTest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestDiscoverKotlin(t *testing.T) {
	got := discovered(map[string]string{
		"Calc.kt": "package com.example.kt\n\nimport org.junit.Test\n\nclass CalcTest {\n  @Test fun `adds numbers`() { val s = \"\"\"class Raw { @Test }\"\"\" }\n  companion object { const val X = 1 }\n  class NestedTest { @Test fun n() {} }\n  inner class InnerTest { @Test fun i() {} }\n}\n",
		"Base.kt": "package com.example.kt\nabstract class BaseTest { @Test fun base() {} }\nclass ChildTest : BaseTest()\ndata class Value(val x: Int)\nenum class Kind { A }\nobject Singleton { @Test fun s() {} }\nannotation class Tag\n",
	})
	want := []string{
		"com.example.kt.CalcTest",
		"com.example.kt.CalcTest$NestedTest",
		"com.example.kt.ChildTest",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestDiscoverDefaultPackage(t *testing.T) {
	got := discovered(map[string]string{"A.java": "public class ATest { @Test public void a() {} }"})
	if !reflect.DeepEqual(got, []string{"ATest"}) {
		t.Errorf("got %q", got)
	}
}
