package com.example.fixture;

import static org.junit.Assert.assertEquals;
import java.util.Arrays;
import java.util.Collection;
import org.junit.Test;
import org.junit.runner.RunWith;
import org.junit.runners.Parameterized;

@RunWith(Parameterized.class)
public class ParameterizedTest {
    @Parameterized.Parameters(name = "value {0}")
    public static Collection<Object[]> values() {
        return Arrays.asList(new Object[][] {{1}, {2}});
    }

    @Parameterized.Parameter
    public int value;

    @Test
    public void addsZero() {
        assertEquals(value, Arithmetic.add(value, 0));
    }
}
