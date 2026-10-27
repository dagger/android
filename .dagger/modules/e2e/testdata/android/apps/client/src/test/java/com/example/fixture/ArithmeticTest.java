package com.example.fixture;

import static org.junit.Assert.assertEquals;
import org.junit.Test;

public class ArithmeticTest {
    @Test
    public void addsNumbers() {
        assertEquals(5, Arithmetic.add(2, 3));
    }
}
